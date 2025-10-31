# Step-by-step human-readable algorithm

*(No code — plain clear steps, all corner cases, and exactly what each thread does.)*

---

## Goal (one line)

Receive RTP packets, reliably reassemble each encoded video frame (H.264/H.265 or similar), and hand completed frames to a separate thread that converts them to JPEG — all while handling packet loss, reordering, fragment reassembly, SSRC/timestamp wrap, and resource limits.

---

## Roles (threads & their responsibilities)

1. **RTP Receiver Thread (1 or more)**

   * Reads RTP packets from the network socket.
   * Does *lightweight parsing* only (header fields, basic payload inspection).
   * Inserts packets into per-stream reassembly structures (jitter buffer / frame assembly).
   * Never performs heavy CPU work or disk I/O.

2. **Frame Reassembler / Jitter-Buffer Manager (can be part of receiver thread or its own thread)**

   * Groups packets by (SSRC, RTP timestamp).
   * Tracks sequence ranges, reorders packets, handles fragment assembly (FU-A/FU-B/STAP etc.).
   * Detects end-of-frame (marker bit or heuristics) and decides when a frame is complete or must be dropped.
   * Pushes completed encoded-frame bytes into the **Frame Queue**.

3. **Frame Queue (thread-safe queue)**

   * Buffer between reassembler and JPEG converter.
   * Bounded size to provide back-pressure.

4. **JPEG Converter Thread(s)**

   * Pulls completed encoded frames from the queue.
   * Ensures SPS/PPS (codec parameter sets) available; prepends cached ones if needed.
   * Decodes encoded frame to an image buffer.
   * Encodes image buffer to JPEG and saves/forwards it.
   * Reports decode failures back to metrics/log.

5. **Housekeeping / Watchdog Thread**

   * Periodically finalizes stale frames (timeouts), prunes buffers, logs metrics, and handles SSRC resets.

6. **(Optional) RTCP / Retransmit Manager**

   * Sends NACKs or RTCP feedback when missing packets are detected and retransmit is supported.

---

## Data model — plain language (what you store and why)

* **Per-SSRC table**: separate workspace for each stream (prevents mixing streams).
* **FrameAssembly** (one per distinct RTP timestamp within a SSRC): holds

  * arrival time of first packet,
  * mapping: sequence number → packet payload,
  * lowest and highest sequence observed,
  * whether any packet for that timestamp had the marker bit,
  * lists/buffers for partially reassembled NALs (fragmented NALs),
  * status flag (collecting / complete / malformed / dropped).
* **Cached codec config**: most recent SPS/PPS (H.264) or VPS/SPS/PPS (H.265) per SSRC.
* **Global small cache of recent (SSRC,SEQ)** to detect duplicates.
* **Frame Queue**: holds (SSRC, timestamp, encoded-frame-bytes, metadata).

---

## Short definitions / important concepts (human terms)

* **RTP sequence number**: 16-bit number, increments per packet; wraps from 65535→0.
* **RTP timestamp**: 32-bit time for samples of encoded frames; same value used for all packets belonging to the same frame. Wraps eventually.
* **Marker bit**: set by sender usually on last packet of a frame — best indicator frame is complete. Not always present.
* **NAL unit**: codec chunk (H.264/H.265); a frame may be multiple NALs. Some NALs are fragmented across multiple RTP packets (FU-A etc.).
* **FU (fragmented unit)**: a fragment of one NAL carried across several RTP packets — must be reassembled in order.

---

## High-level decision rules you must implement (plain language)

1. **Group by SSRC and timestamp.** All packets with the same SSRC and RTP timestamp belong to the same encoded frame (unless the sender uses different mapping — follow SDP if present).
2. **Reorder by sequence number taking wrap into account.** Treat sequence numbers as circular; if comparing two seq numbers, consider the 16-bit circular difference — the nearer number is "less".
3. **Wait a small reordering window** (e.g., 20–80 ms) for late packets before finalizing — tune per network.
4. **Use marker bit when present** as immediate finalization signal for that frame.
5. **If marker missing, detect frame end by timestamp change** or by timeout (forced finalize after MAX_JITTER_BUFFER_MS).
6. **Reassemble fragmented NALs only when you have all fragments (start+middle+end).** If start is missing but you get middle/end, buffer until timeout — if start never arrives, mark that NAL/frame corrupted.
7. **If missing packets after reasonable wait**:

   * Option A (if RTCP NACK supported): request retransmit and wait a short retry window.
   * Option B (no retransmit): either drop the frame or attempt partial reassembly — but partial frames often decode incorrectly; prefer to drop if a critical fragment (SPS/PPS/IDR) missing.
8. **Always maintain resource caps**: max packets per frame, max memory for outstanding frames, frame queue size. Drop least-important frames (non-key) when overloaded.
9. **When decoding, ensure SPS/PPS present.** If missing in the frame, prepend cached SPS/PPS from previous frames or from signaling before decode.

---

## Very detailed step-by-step algorithm (human steps, numbered)

### Preparation / constants (choose and tune)

* Decide values:

  * `Reorder wait` (how long to wait for out-of-order packets): e.g., 40 ms.
  * `Max jitter buffer`: e.g., 120–200 ms.
  * `Frame finalize timeout`: e.g., 120 ms (force finalize if no marker).
  * `Frame queue size`: e.g., 50–200.
  * `Max packets per frame` and `Max frame bytes` to avoid OOM.

### 1 — Packet arrival (receiver thread)

1. Read packet bytes from UDP socket. Record arrival monotonic time.
2. Parse RTP header fields: sequence number, timestamp, SSRC, marker bit, payload type.
3. Quick validations:

   * If payload length is zero, drop and log.
   * If this (SSRC, seq) was seen recently, drop duplicate.
4. Look up the per-SSRC table. If none, create a new SSRC entry and set last-seen info.
5. In SSRC table, look up FrameAssembly for this packet’s timestamp. If none, create it and set its `first-receive-time` = arrival time.
6. Insert the packet’s payload into the FrameAssembly mapped by sequence number. Update `seq_min` and `seq_max`.
7. Do lightweight payload inspection:

   * If payload looks like SPS/PPS or codec config, store a copy in the SSRC cached codec-config.
   * If payload is a single NAL or STAP-A, store it (with a marker that it’s a full NAL) keyed by this sequence for later ordering.
   * If payload is a FU (fragment), store the fragment in the FU buffer keyed by a "NAL identifier" that includes timestamp and reconstructed NAL type. *Do not attempt to full-assemble FU at this point*; just record fragments by sequence.
8. If packet.marker == true, set `frame.expected_end = true`.

### 2 — Decide whether to attempt finalization (receiver or reassembler)

After insert, check these conditions in this order:

* If `frame.expected_end == true`: attempt finalize immediately (marker indicates end).
* Else if a newer packet arrived for the same SSRC with a different timestamp and sufficient time passed since frame’s first packet (reorder wait): attempt finalize older frame.
* Else if `now - frame.first_receive_time >= Frame timeout` (force finalize): attempt finalize as partial frame.

If none apply, continue receiving more packets for that frame.

### 3 — Frame finalize steps (when triggered)

Goal: produce a single contiguous encoded frame (NAL stream) from collected data or decide to drop/mark malformed.

1. **Make list of observed sequence numbers** between `seq_min` and `seq_max`. Determine any missing sequence numbers in that range.
2. **If missing sequences exist**:

   * If `now - first_receive_time < Reorder wait` → wait longer.
   * If retransmit (RTCP NACK) supported and you haven't requested yet → send NACK and wait short retry window.
   * If retransmit not possible or retries exhausted:

     * If missing fragments belong to a FU that contains an essential NAL (SPS, PPS, IDR): mark frame as **dropped** or **malformed** and do not deliver to decoder (prefer dropping).
     * If missing fragments affect non-critical NALs (e.g., non-reference slices), you may attempt partial reassembly and deliver — but expect decode artifacts.
3. **Reorder packets** by sequence number using wrap-aware comparison (treat sequence numbers modulo 65536; choose ordering that minimizes circular distance).
4. **Reconstruct NAL units in order**:

   * For every ordered packet:

     * If it contains a whole NAL, append it (remember to add start-code if your decoder expects Annex-B).
     * If it contains STAP (multiple NALs), split and append each.
     * If it is an FU fragment:

       * Collect all fragments for that FU (by matching start/middle/end flags); ensure `start` fragment exists and `end` fragment exists and internal seqs are present. If fragments missing, mark that NAL corrupted.
       * On successful reassembly, reconstruct original NAL header + payload and append as a single NAL.
5. **Validation checks**:

   * Total reassembled size must be below max permitted.
   * At least one valid NAL should exist.
   * If frame is an IDR/keyframe but SPS/PPS are missing: try to prepend cached SPS/PPS from SSRC; if none, mark as undecodable.
6. **If frame passes validation**: mark `frame.status = complete` and create `encoded_frame_bytes` by concatenating NALs in order.
7. **Push `encoded_frame_bytes` and metadata (SSRC, timestamp, arrival times, flags like keyframe)** into the **Frame Queue** non-blocking:

   * If the queue is full, decide policy:

     * Drop this frame and log, or
     * Block briefly (not recommended, blocks receiver) — better to drop non-key frames to protect keyframes.
8. **Cleanup**: remove FrameAssembly from in-memory structures, free per-packet buffers.

### 4 — JPEG Converter thread(s) behavior (consumer)

1. Loop: take next `(SSRC, timestamp, encoded_frame_bytes, metadata)` from Frame Queue.
2. **Ensure codec parameters**:

   * If decoder requires SPS/PPS and they are not in `encoded_frame_bytes`, fetch cached SPS/PPS for that SSRC and prepend before decode.
3. **Decode** encoded bytes into one or more raw image frames using a robust decoder (FFmpeg/GStreamer/PyAV).

   * If decode fails due to corruption, log and optionally save encoded bytes for debugging; skip saving JPEG.
4. **Convert decoded image to color buffer** (e.g., BGR or RGB).
5. **Encode to JPEG** with configured quality.
6. **Save JPEG** to disk or forward to target destination:

   * Use atomic file write pattern: write to temporary file then rename to final to avoid partial reads.
   * Use organized naming: include SSRC, RTP timestamp, monotonic time, and frame counter.
7. **Report metrics**: successful saves, decode errors, average time.

### 5 — Housekeeping tasks (periodic)

Run every ~500–1000 ms:

1. For each SSRC and FrameAssembly still `collecting`:

   * If `now - first_receive_time >= MAX_JITTER_BUFFER_MS` and not yet finalized → call finalize with `allow_partial=True`.
2. Remove old frames and prune memory.
3. Clear duplicate detection cache older than a few seconds.
4. If Frame Queue occupancy > high-water mark → set a flag to reassembler to prefer dropping non-key frames.
5. Rotate / update cached SPS/PPS if changed (store time of last update).
6. Collect metrics and log aggregates.

### 6 — Handling SSRC changes & stream restarts

1. Detect SSRC change by:

   * Receiving a packet whose SSRC is new or by a sudden large jump in sequence numbers for a known SSRC (indicating restart).
2. On detection:

   * Gracefully flush outstanding FrameAssemblies for old SSRC after a short grace (e.g., 100 ms); mark as dropped.
   * Reset per-SSRC state (last_seq, cached codec config if sender likely restarted) — but keep cached SPS/PPS for a short time as it may still be useful.
   * Start new per-SSRC entries.

### 7 — Handling timestamp and sequence wrap-around

1. Sequence numbers are 16-bit: when comparing ordering, always interpret them circularly. The safe heuristic: if difference modulo 65536 is less than 32768, it's forward; else backward.
2. RTP timestamps are 32-bit: treat timestamp ordering circularly when needed; compute absolute wall time by using RTP clock rate and initial offset if you need real time mapping.

---

## Explicit corner cases and exactly what to do for each

1. **Out-of-order packets**

   * Wait small reordering window (e.g., 40 ms). Reorder by sequence using circular comparison.

2. **Duplicate packets**

   * Detect via (SSRC, seq) cache. Discard duplicates.

3. **Missing packets (loss)**

   * If RTCP NACK available: request retransmit. Wait short retry window.
   * If no retransmit: if missing fragment is critical (SPS/PPS/IDR), drop frame. If not critical, attempt partial decode or drop based on policy.

4. **Fragmented NALs arriving out-of-order / missing start**

   * Buffer fragments keyed by the NAL identifier. If `start` never arrives within timeout, discard fragments and mark NAL corrupted.

5. **Marker bit missing (sender doesn’t set it)**

   * Use timestamp change or timeout to detect frame boundary. When a packet arrives with a new timestamp, finalize older timestamp(s) after reordering window unless waiting for late packets.

6. **SPS/PPS not present in every frame**

   * Cache last SPS/PPS per SSRC. Prepend them before decoding frames that lack them. If none available, skip decoding and save encoded bytes for debugging.

7. **SPS/PPS change mid-stream**

   * On detection of new SPS/PPS, mark decoder re-initialization needed. Prepend fresh ones to subsequent frames and reinitialize decoder in converter thread safely.

8. **SSRC collision or restart**

   * Detect sudden sequence/timestamp discontinuity. Flush and reset buffers; keep short grace period.

9. **Large frames / resource exhaustion (DoS)**

   * Enforce limits: max packets/frame and max bytes/frame. If exceeded, mark frame dropped and optionally log/alert.

10. **FEC present**

    * If using forward error correction, attempt FEC recovery before deciding to drop.

11. **High converter backlog**

    * If Frame Queue is near full, instruct reassembler to drop non-key frames to prioritize keyframes.

12. **Decoder failure on a frame**

    * Log and optionally persist the encoded bytes. Continue with next frame; do not crash.

13. **Multiple encoded frames decode to multiple images**

    * If a single encoded payload decodes to multiple raw frames, either save all or select the first based on metadata.

---

## Safety and correctness checks you should enforce continuously

* Do not block the RTP receiver for disk I/O or decoding.
* Always use monotonic time for timeouts.
* Always cap memory usage for outstanding frames and packet storage.
* Prefer dropping to deadlocking: if a buffer is full, drop least-important frames (non-key).
* Persist malformed frames for post-mortem if debugging.
* Maintain metrics (packet loss %, reassembly time, frames dropped, queue waits) to tune thresholds.

---

## Useful tuning guidelines (practical)

* LAN / stable: small reorder wait (10–40 ms), low jitter buffer (40–80 ms).
* Internet / mobile: larger reorder wait (80–200 ms), higher jitter buffer.
* High-res frames: allow bigger `MAX_FRAME_BYTES` but also limit packets/frame.
* CPU-limited: increase converter threads if CPU allows; otherwise lower frame queue size and drop more non-key frames.

---

## Example timeline for a single frame (how it flows, step-by-step)

1. Receiver gets packet A (seq 1000, ts 12345), creates FrameAssembly ts=12345, stores A.
2. Receiver quickly gets packet B (seq 1001), stores B.
3. Packet C (seq 1003) arrives before seq 1002 (out-of-order). Reassembler waits reorder window for seq 1002.
4. Seq 1002 arrives within window. Marker bit on seq 1003 = true → frame.expected_end = true.
5. Reassembler orders 1000→1003, sees FU fragments and completes FU reassembly into NALs, prepends cached SPS if missing, validates size.
6. Encoded bytes placed on Frame Queue.
7. Converter thread pulls frame, decodes to raw image, encodes to JPEG, saves as `ssrc_ts_mono.jpg`.
8. Metrics updated, FrameAssembly cleaned up.

---

## Checklist for a robust deployment (what to implement & verify)

* [ ] Per-SSRC isolation and cached SPS/PPS.
* [ ] Circular sequence and timestamp comparison.
* [ ] Reorder wait + finalize timeout.
* [ ] FU fragment buffering and complete-only reassembly.
* [ ] Non-blocking push to Frame Queue with back-pressure policy.
* [ ] Converter thread(s) that handle decoder init, SPS/PPS prepend, decode errors.
* [ ] Housekeeping thread for timeouts, pruning, metrics.
* [ ] RTCP NACK support (optional, for lower loss).
* [ ] Limits for packets per frame and total outstanding frames.
* [ ] Logging of malformed frames and option to save them for offline debugging.
* [ ] Shutdown sequence: stop receiver → drain queue → join converter threads → free resources.

