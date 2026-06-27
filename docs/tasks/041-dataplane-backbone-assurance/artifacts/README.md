# Evidence artifacts (Task 041)

Captured red-run outputs for the AC-9 one-time mutation kill-tests live here:

- `mutation-pending-key.txt` — swapped/duplicated `pending`-map key → crosstalk test red
- `mutation-no-drop-on-full.txt` — removed drop-on-full → producer-hang test red
- `mutation-no-liveness-reap.txt` — disabled liveness reap → dead-peer/Serve test red

Each is captured during execution, then the mutation is reverted (one-time evidence, not a permanent CI gate).
