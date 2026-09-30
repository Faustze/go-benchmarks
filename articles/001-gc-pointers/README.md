# 001 · pointers and garbage collection: a 10M-entry cache

## Question

How much wall-clock time and CPU does a single timer-triggered GC cycle cost in Go 1.27.1 when memory holds a 10M-entry cache: a pointer-based LRU versus a map with the same data and not a single pointer. As a separate line: what Green Tea GC changes.

After the first runs the question grew: two intermediate layouts (P and B2) were added, plus a quick `runtime.GC()` series from 100K to 10M entries to see what drives the cost of marking.

Page with interactive charts: https://go.faustze.tech/001-gc-pointers/

## Context

If there has been no GC cycle for 2 minutes, the runtime starts one itself ([`forcegcperiod`, runtime/proc.go](https://github.com/golang/go/blob/go1.27.1/src/runtime/proc.go#L6522-L6527)). A service that barely allocates only gets these cycles, and each one traverses everything live in the heap. In 2020 discord got CPU spikes and latency every 2 minutes because of this ([Why Discord is switching from Go to Rust](https://discord.com/blog/why-discord-is-switching-from-go-to-rust)). This measurement checks the same mechanism on current Go.

## Environment

> filled in on the machine where the runs happened

|                                     |                              |
| ----------------------------------- | ---------------------------- |
| CPU                                 | Intel Core i7-6700 @ 3.40GHz |
| Cores / threads                     | 4 / 8                        |
| Memory                              | 31 GiB                       |
| OS, kernel                          | Linux 6.8.0-142-generic      |
| WSL2                                | no, native Linux             |
| Go                                  | go1.27.1 linux/amd64         |
| `GOAMD64`                           | v1                           |
| `GOMAXPROCS` (`# P` field in gctrace) | 8                          |

`./articles/001-gc-pointers/run.sh env` captures the environment into `logs/env.txt`. Run it from inside the repository so that `go version` reports the toolchain from `go.mod`.

## Hypothesis

> written before the first run

**How many times A's mark CPU exceeds B's.** Orders of magnitude. The timer triggers GC for both variants the same way, every 2 minutes, but in B the pointer-free map is marked noscan, and marking does not go inside it. For A, marking traverses about 60M pointers and marks about 30M objects (`entry`, `ReadState` and the key bytes for every entry). For B, a map in Go 1.24+ is a directory of tables with ≤1024 slots each: for 10M entries that is on the order of 10–16K tables, and the collector walks only the directory and table headers, tens of thousands of pointers. The group arrays holding the `uint64 → ReadState` pairs themselves are marked live without scanning. What remains is stacks, globals and the fixed per-cycle overhead (two STW pauses, starting workers), the same for both variants. By pointers the difference is about 1000×; in milliseconds I expect 100–1000×: for B the fixed per-cycle overhead will eat the time.

**How long marking takes for A.** Estimate: number of pointers × cost of checking one. 10M entries × 6 pointers = 60M. At 1 ns per pointer (a lower bound, when the data is in the CPU cache) that gives 60 ms of CPU. Wall-clock time is roughly half, about 30 ms: background marking runs on 25% of 8 threads, i.e. on two. The entries are scattered across ~1.5 GB and do not fit in the CPU cache, so the real number is likely higher.

**Interval between GC cycles.** Exactly 120 s: `forcegcperiod` in the runtime is 2 minutes, and a cycle starts if there has been none for longer than that.

**What Green Tea changes.** For A marking gets cheaper by percent, not by multiples: the number of pointers is the same, only the memory traversal order changes. In the [Go 1.26 release notes](https://go.dev/doc/go1.26) the Go team expects a 10–40% reduction in GC overhead for programs that put heavy load on the collector. Another ~10% is promised on Intel Ice Lake and AMD Zen 4 CPUs, but the i7-6700 (Skylake) is older, so I don't expect that part. For B there will be almost no difference: there is nothing to scan.

## Methodology

### Data

All variants store the same `ReadState` entries with two fields: `LastReadID uint64` and `MentionCount uint32` (16 bytes with alignment).

| Variant | How entries are stored | Pointers per entry | Heap objects for 10M |
| ------- | ---------------------- | ------------------ | -------------------- |
| A, discord-style LRU | `map[string]*entry`, key `strconv.Itoa(i)`. `entry` holds `key string`, `value *ReadState`, `prev` and `next *entry`; all entries are linked into a doubly linked list, as in an LRU | 6: 4 in `entry` and 2 in the map slot | 25M |
| P, map of pointers | `map[uint64]*ReadState` | 1 | 10M |
| B2, string key | `map[string]ReadState`, key `strconv.Itoa(i)` | 1: the key bytes | 5M (the tiny allocator packs two short keys into one 16-byte block) |
| B, flat map | `map[uint64]ReadState` | 0 | 33K (map directory and tables) |

There is no eviction: the cache is filled once and never changes afterwards.

### Go builds

| Build   | Command |
| ------- | ------- |
| green   | `go build` (Green Tea is on by default) |
| nogreen | `GOEXPERIMENT=nogreenteagc go build` |

### Run

1. Fill the cache of the selected variant.
2. Call `runtime.GC()` so that the timer counts from a known point, and print a `=== FILLED ... ===` marker to stderr with the object count and heap size from `runtime.MemStats`.
3. `timer` mode: 11 minutes of idling without allocations (`time.Sleep`), GC cycles are started only by the timer. `forced` mode: eight `runtime.GC()` calls with a 200 ms pause.
4. After idling, access the cache and call `runtime.KeepAlive` so the cache stays live until the end of the run.

Program flags: `-variant` (`a`, `p`, `b2`, `b`), `-n` (number of entries), `-mode` (`timer` or `forced`), `-idle` (idle time in `timer` mode), `-forced` (number of `runtime.GC()` calls). The program writes gctrace and markers to stderr, `run.sh` redirects it to `logs/<series>/<variant>-<build>-<N>.log`, so `GODEBUG=gctrace=1` lines and markers end up in one file in order. Max RSS is captured with `/usr/bin/time -v` into `*_time.log`. Runs go strictly one after another.

### Series

| Series | Configurations | Runs | Counted GC cycles per run |
| ------ | -------------- | ---- | ------------------------- |
| `scaling` (`runtime.GC()`) | 4 variants × 5 sizes (100K, 300K, 1M, 3M, 10M) × green/nogreen | 1 each | 7 |
| `timer` (11 min idle, 10M) | A, P, B2, B · green; A, B · nogreen | 3 each | 4 |

### What is counted

- Only GC cycles after the marker are taken. In the `timer` series gctrace prints a `GC forced` line before each such cycle: it means the timer started the cycle. The parser checks this for every cycle.
- The first GC cycle after the marker in each run is a warm-up and is not counted.
- Fields of a `gctrace` line (`gc # @#s #%: #+#+# ms clock, #+#/#/#+# ms cpu, #->#-># MB, ...`):
  - mark, wall-clock: the second term in `ms clock`;
  - mark, CPU: assist + background + idle, the three middle numbers in `ms cpu`;
  - STW pauses: the first and third terms in `ms clock`;
  - live heap: the third number in `#->#-># MB`;
  - interval between GC cycles: the difference between adjacent `@#s`.
- For each configuration the median (`gonum/stat.Quantile`, empirical), minimum and maximum are computed over all GC cycles from all runs.

### Running

```sh
./articles/001-gc-pointers/run.sh build     # bin/gcexp-green and bin/gcexp-nogreen
./articles/001-gc-pointers/run.sh env
./articles/001-gc-pointers/run.sh scaling   # ~10 min
./articles/001-gc-pointers/run.sh timer     # ~2.5 h, better in tmux
./articles/001-gc-pointers/run.sh report    # data.json and img/*.png
```

The parser and charts live in `report/`: `parse.go` parses the logs, `plots.go` draws PNGs with `gonum/plot`. The measurement has its own `go.mod`: gonum/plot is only needed by `report/`, the site does not need it. The article text is in `index.ru.md`; the site generator takes numbers, tables and charts from `data.json` at build time.

### Limitations

- An idle program is not a service under load. The measurement shows the cost of a single GC cycle, not the latency a user sees.
- Background marking takes 25% of `GOMAXPROCS`, so wall-clock mark time depends on the number of cores. Numbers are compared only within the same environment.
- The `scaling` series is a single run per configuration. The green/nogreen difference in it should be read as an observation, not a result.

## Results

All GC cycles after the marker in the `timer` series were started by the timer: each is preceded by a `GC forced` line, and the parser checks this for every one. Full per-cycle data is in `data.json`, charts are on the [measurement page](https://go.faustze.tech/001-gc-pointers/).

### Timer-triggered GC, 10M entries, 11 minutes idle

| Configuration | Runs | Counted cycles | Interval, s (min / max) | mark clock, ms (median / max) | mark CPU, ms (median / max) | STW max, ms | Live heap, MB | Max RSS, MB |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| A · green | 3 | 12 | 120.9 / 122.2 | 974 / 1,297 | 1,949 / 2,594 | 0.058 | 1,113 | 1,378 |
| P · green | 3 | 12 | 120.5 / 120.6 | 515 / 550 | 1,030 / 1,101 | 0.038 | 441 | 544 |
| B2 · green | 3 | 12 | 120.4 / 121.7 | 364 / 389 | 728 / 778 | 0.050 | 716 | 842 |
| B · green | 3 | 12 | 120.0 / 120.0 | 1.2 / 2.9 | 1.3 / 2.2 | 0.049 | 426 | 527 |
| A · nogreen | 3 | 12 | 121.8 / 124.5 | 2,668 / 3,100 | 2,839 / 3,630 | 0.077 | 1,113 | 1,363 |
| B · nogreen | 3 | 12 | 120.0 / 120.0 | 2.0 / 2.6 | 2.0 / 2.7 | 0.074 | 426 | 523 |

### `runtime.GC()` series, 10M entries

One run per configuration, 7 counted GC cycles. Sizes from 100K to 3M are in `data.json` and on the page.

| Configuration | mark clock, ms | mark CPU, ms (median, min…max) | Heap objects | Live heap, MB | CPU per object, ns |
| --- | --- | --- | --- | --- | --- |
| A · green | 382 | 3,028 (2,951…3,156) | 25,015,418 | 1,113 | 121 |
| P · green | 150 | 1,114 (1,101…1,153) | 10,032,983 | 441 | 111 |
| B2 · green | 134 | 1,014 (992…1,059) | 5,015,427 | 716 | 202 |
| B · green | 1.2 | 1.4 (1.1…1.9) | 32,976 | 426 | 42 |
| A · nogreen | 482 | 3,846 (2,584…5,077) | 25,015,421 | 1,113 | 154 |
| P · nogreen | 102 | 797 (403…812) | 10,032,982 | 441 | 79 |
| B2 · nogreen | 87 | 688 (578…703) | 5,015,419 | 716 | 137 |
| B · nogreen | 0.75 | 3.1 (2.9…3.7) | 32,981 | 426 | 94 |

![mark CPU for 10M entries](img/01-mark-cpu-10m.png)

![timer-triggered GC cycles](img/05-timer-timeline.png)

![Green Tea](img/04-green-tea.png)

## Conclusion

**How many times A's marking is more expensive than B's.** Hypothesis: 100 to 1000× in milliseconds. Fact: under the timer 1,949 vs 1.3 ms CPU, about 1,500×; with `runtime.GC()` about 2,200×. The order of magnitude matched; the number landed at the upper bound of the forecast and slightly above.

**What marking costs for A.** Hypothesis: 60 ms CPU and about 30 ms wall-clock. Fact: under the timer 1.9 s CPU and 0.97 s wall-clock, 30× more. The "nanosecond per pointer" model did not fit. In this measurement the cost is better explained by the number of live objects: about 80 ns CPU per object under the timer and 120 ns with `runtime.GC()`, which is the order of a CPU cache miss. On the "objects, not bytes" chart all four variants fall close to a single line by heap object count (`runtime.GC()` series, one run per point), while the live heap size does not predict the cost: B at 426 MB is marked 800× cheaper than P at 441 MB.

**String key.** B2 stores the same pointer-free values as B, but the `string` key makes the map scannable: 1,014 ms CPU vs 1.4 ms with `runtime.GC()`. B2 has 5M objects for 10M keys because the tiny allocator puts two short keys into one 16-byte block.

**Interval between GC cycles.** Hypothesis: exactly 120 s. Fact: 120 s plus the duration of the previous cycle. B's interval is 120.0 s, A's from 120.9 to 122.2 s, A without Green Tea up to 124.5 s. The countdown starts at the end of a cycle: `last_gc_nanotime` is written in `gcMarkTermination` ([runtime/mgc.go](https://github.com/golang/go/blob/go1.27.1/src/runtime/mgc.go#L1437)), and the timer check compares the current time against it ([same file](https://github.com/golang/go/blob/go1.27.1/src/runtime/mgc.go#L718-L719)).

**Green Tea.** Hypothesis: a gain in percent for A, no change for B. Fact under the timer: for A 974 vs 2,668 ms wall-clock (2.7×) and 1,949 vs 2,839 ms CPU (1.5×). The difference between these numbers is visible in gctrace. With Green Tea, CPU in every cycle is exactly twice the wall-clock time, i.e. both background workers are running. Without Green Tea, in 14 cycles out of 15 CPU equals wall-clock time, effectively only one is running. This is an observation of this measurement, not a general property of Green Tea; the cause is listed in the open questions. With `runtime.GC()` on eight workers A gains about 21% in CPU; for P and B2 the median with Green Tea is higher, but that is one run per configuration and nothing can be claimed here. For B the difference is 1.2 vs 2.0 ms, negligible in absolute terms.

**Rule of thumb.** In this measurement the cost of marking is roughly the number of live objects times memory latency: almost every pointer hop in a large heap misses the CPU cache. On the i7-6700 that came out to about 100 ns CPU per object (for 10M entries, from 39 ns for B under the timer to 202 ns for B2 with `runtime.GC()`). This figure is not a Go GC constant: the cost also depends on the size of the scanned part of an object and on how objects are laid out in memory; other hardware and other data structures will give a different number. Here a million long-lived objects cost on the order of 100 ms of CPU per GC cycle. The rule applies only to long-lived data: temporary objects are dead by the time a cycle runs, and marking does not traverse them.

**What this means for a service.** While idle, cache A takes two of eight logical CPUs for about a second every two minutes. Under load, marking shares CPU with requests, and goroutines that start allocating during that second may be hit by mark assist and pay for marking themselves (not measured under load). Regular `GOGC` and `GOMEMLIMIT` values do not cancel timer-triggered GC. `GOGC=off` disables the timer ([runtime/mgc.go](https://github.com/golang/go/blob/go1.27.1/src/runtime/mgc.go#L715-L717)), but with `GOMEMLIMIT` set, GC cycles will still come when the heap reaches the limit. The pointer-free layout (B) removes the problem entirely in this measurement: 1.2 ms per GC cycle with the same data.

**Open questions.**

- Why, without Green Tea, marking while idle almost always runs on one background worker out of two.
- Why `sysmon` is not late to start GC: in full idle it can sleep up to `forcegcperiod / 2`, yet B's interval is 120.02 s in every cycle.
- Why CPU per object under the timer (78 ns) is lower than with `runtime.GC()` (121 ns). A possible explanation: eight workers contend on the memory bus. Not verified.
- Whether the Green Tea gain holds on another CPU: the i7-6700 (Skylake) predates the architectures for which the Go team promised an extra 10%.
