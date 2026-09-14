Kutip **worst-case** (atau amortized). Jangan jual best-case beruntung sebagai Big O algoritma.

- Match SKU/katalog/nama: satu pass atau map/index. Jangan nested scan katalog × token (`O(n·m)` per turn).
- “Sudah di keranjang / sudah dilihat”: set atau map, bukan `contains` list di dalam loop.
- Jangan sort hanya untuk min/max atau cek keanggotaan — satu pass `O(n)`.
- Jangan HTTP/DB/query di dalam loop item.
- Rekursi harus mengecil; subproblem overlap wajib memo. Jangan rekursi tak terbatas pada token pesan.
- Concat string/slice di hot loop: builder atau pre-size (hindari copy `O(n²)`).
- Katalog produksi tumbuh; prefer `O(n)` / `O(n log n)`, bukan `O(n²)` pada katalog × token meski fixture tes kecil.
- Jika algoritma lebih lambat wajib (urutan FSM, stabilitas), jangan tambah nested scan kedua.
- Diff minimal. Jangan micro-opt bit-twiddle. Correctness tes golden item/qty/path dulu.
