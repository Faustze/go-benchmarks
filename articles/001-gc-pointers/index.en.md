+++
title = "What a pointer costs"
description = "The discord case on Go 1.27.1: the cost of marking in the garbage collector for a 10M-entry cache with and without pointers, and what to do about it."
date = 2026-09-30
go = "1.27.1"
hardware = "i7-6700 · GOMAXPROCS=8"
translation = "machine"
source_hash = "92291bd5fc737e09"
+++

If a service keeps millions of objects in memory (a cache, an in-memory index), the collector traverses them on every GC cycle. It starts from the roots, follows pointers, and reads the pointer fields of every live object. This benchmark checks the mechanism from the discord case on Go 1.27.1. It uses a synthetic cache with no requests and no eviction: the same 10 million entries in four layouts. The first sections cover what to do and how to check your own service. The charts are at the end.

{{< kpis >}}

**What the numbers mean.** Mark/scan wall-clock time: how long background marking lasts; the program keeps running during it. Mark CPU: how much CPU time marking consumed. STW: how long the program actually stopped, here no more than {{< stw >}} per GC cycle. Live heap: how much memory is in use after marking. The other phases of the GC cycle are short, so below, "GC cycle" almost always means its marking.

## What to do

This applies only to data that is long-lived and exists in millions of instances. Temporary per-request objects are already dead by the time a GC cycle runs, so the collector does not traverse them. The effects below were measured on timer-triggered GC with 10 million entries (mark/scan wall-clock time).

### Numeric key, entry by value

`map[string]*Entry` → `map[uint64]Entry`

The string key is turned into a unique numeric id, and the entry is stored by value with no pointers inside. A string hash can replace the id only if collisions are ruled out or handled. In the benchmark, "before" is a discord-style LRU: such a map plus a doubly linked list. Effect: {{< num "timer.a.green.mark_clock_ms" ms >}} → {{< num "timer.b.green.mark_clock_ms" ms >}} per GC cycle.

### Value instead of pointer

`map[K]*T`, `[]*T` → `map[K]T`, `[]T`

This removes one object and one pointer per entry. It works if T is small and contains no pointers, and if the entry needs no separate identity: nobody holds a `*T`, and the entry is replaced as a whole via `m[k] = v`. Cost: T is copied on every read and update, which is expensive for a large T. Effect: {{< num "timer.p.green.mark_clock_ms" ms >}} → {{< num "timer.b.green.mark_clock_ms" ms >}} per GC cycle.

### Key without a string

`map[string]T` → `map[uint64]T`

A string holds a pointer to its bytes. A string key alone makes the collector traverse the whole map, even if the values are flat. Effect: {{< num "timer.b2.green.mark_clock_ms" ms >}} → {{< num "timer.b.green.mark_clock_ms" ms >}} per GC cycle.

### Fields without hidden pointers

```go
// before
CreatedAt time.Time
ID        string
Meta      any
// after
CreatedAt int64
ID        [16]byte
Meta      Meta
```

`time.Time` hides a `*Location`, and `string` and `any` also contain pointers. One such field makes the whole struct scannable. `int64` (for example, Unix nanoseconds) works if you only need the point in time: the time zone and the monotonic clock reading are lost. Not benchmarked separately; the mechanism is the same.

### Links via indices

`prev, next *entry` → `prev, next int32 // indices into []entry`

Lists and trees (an LRU is also a list) can be kept in a slice of nodes. In the benchmark LRU, `prev` and `next` are 2 of the 6 pointers per entry. Cost: you have to track freed slots yourself, for example with a free list. Not benchmarked separately.

### Strings in one buffer

`[]string` → `buf []byte` and `span []struct{ off, n uint32 }`

The bytes of all strings live in a single `[]byte`, and each entry stores an offset and a length. bigcache and freecache work this way. Cost: deleted strings leave holes, so the buffer has to be compacted. Not benchmarked separately.

> **What doesn't help.** Ordinary `GOGC` and `GOMEMLIMIT` values change how often heap growth triggers GC. They do not stop timer-triggered GC every two minutes, and each such cycle traverses all live objects. `GOGC=off` also disables the timer. If `GOMEMLIMIT` is set, GC runs when the heap reaches the limit; without a limit, the heap grows with no GC at all. This comes from the runtime code and the [GC guide](https://go.dev/doc/gc-guide); not benchmarked separately.

## How to estimate the cost for your service

In this benchmark, the number of live objects explained the cost of marking best: on the i7-6700 it came to about 100 ns of CPU per object. This figure is specific to my hardware and my data structures (for 10 million entries across layouts and modes it came to {{< nsrange >}} ns). It is not a constant of the Go GC. The cost also depends on the size of the scanned part of each object and on how scattered the objects are in memory, so another CPU will give a different figure. As a rough estimate: on the i7-6700, a million long-lived objects cost on the order of 100 ms of CPU per GC cycle.

**1. How many objects live on the heap.** A standard library metric:

```go
s := []metrics.Sample{{Name: "/gc/heap/objects:objects"}}
metrics.Read(s) // import "runtime/metrics"
n := s[0].Value.Uint64()
fmt.Printf("~%v CPU per GC\n", time.Duration(n)*100) // ~100 ns per object
```

**2. Where they were allocated.** A heap profile by number of live objects. `top` shows the functions that allocated the most live objects. The profile does not show what retains them; you have to find that in the code:

```sh
go tool pprof -sample_index=inuse_objects http://localhost:6060/debug/pprof/heap
```

**3. How long a GC cycle actually takes.** The second term in `ms clock` is the mark/scan wall-clock time.

**4. Whether a GC cycle was timer-triggered.** gctrace prints the line `GC forced` before such a cycle:

```sh
GODEBUG=gctrace=1 ./app 2>&1 | grep -A1 "GC forced"
gc 15 @128.984s 0%: 0.047+1001+0.004 ms clock, ...
```

## One GC cycle, four layouts

Median mark/scan wall-clock time for timer-triggered GC while the program is fully idle. The data is the same; only the number of pointers per entry changes. The spread over {{< num "timer.a.green.gcs" n0 >}} GC cycles is in parentheses. The scale is logarithmic: on a linear scale, the flat map would not be visible at all.

{{< chart hero >}}

## The number of objects explains the cost

20 runs with `runtime.GC()`, from 100 thousand to 10 million entries, one run per point. This is an observation on a single workload, not a general law. By memory size, the points span three orders of magnitude: the flat map at {{< num "forced.b.green.live_mb" n0 >}} MB costs {{< num "forced.b.green.mark_cpu_ms" ms >}} of CPU, and the pointer map at {{< num "forced.p.green.live_mb" n0 >}} MB costs {{< num "forced.p.green.mark_cpu_ms" ms >}}. By number of objects, all variants fall close to a single line.

{{< chart objects >}}

## The discord case: a GC cycle every two minutes

The program fills the cache and then does nothing for 11 minutes. GC cycles still run every two minutes, and each time the LRU occupies two of the eight logical CPUs for about a second. Hollow points: without Green Tea.

{{< chart timeline >}}

## Green Tea made it faster, but the gap remains

Since Go 1.26, the new collector is enabled by default. In this benchmark, it made the LRU GC cycle {{< ratio "timer.a.nogreen.mark_clock_ms" "timer.a.green.mark_clock_ms" n1 >}} times faster in wall-clock time. The flat map is still hundreds of times cheaper. Point: median; line: minimum to maximum.

{{< chart green >}}

## Where pointers hide in Go

The collector looks inside an object if its type has at least one pointer field. It skips types without pointers entirely (numbers, `bool`, arrays of them, structs made of such fields).

| Type | Pointers | What's inside |
|---|---|---|
| `int, float64, bool, [16]byte` | no | Numbers and arrays of numbers. The collector marks the object as live and does not look inside. |
| `struct{ ID uint64; N uint32 }` | no | A struct made of numbers is also noscan. This is how ReadState is stored in the flat map. |
| `string` | yes | A pointer to the bytes plus a length. A `map[string]T` key makes the collector traverse the whole map: when idle, it is roughly {{< ratio "timer.b2.green.mark_clock_ms" "timer.b.green.mark_clock_ms" r10 >}} times more expensive than `map[uint64]T` with the same values. |
| `[]T` | yes | A pointer to the array, a length and a capacity. A slice of numbers has one pointer per slice; the array itself is noscan. |
| `*T, map, chan, func` | yes | Each of these is a pointer. `map[K]V` is scanned only if K or V contains pointers. |
| `interface{}, any, error` | yes | Two words: a type and a pointer to the value. An `any` field makes the struct that holds it scannable. |
| `time.Time` | yes | Contains `loc *Location`. A struct with a `time.Time` field is no longer noscan. A cache can store Unix time in an `int64` if it doesn't need the time zone and monotonic time. |
| struct with one pointer | yes | One pointer field is enough to make the collector read the whole object and follow the reference. |

<details>
<summary>Details: methodology and all numbers</summary>

## Methodology

Two modes. **Timer**: 10 million entries, 11 minutes idle with no allocations, GC triggered only by `forcegcperiod`, three runs per configuration. **`runtime.GC()`**: 8 consecutive calls at sizes from 100 thousand to 10 million, one run per configuration. The figures come from `GODEBUG=gctrace=1`. The first GC cycle after filling the cache is excluded.

The interval between GC cycles is 120 s plus the duration of the previous cycle, because the runtime counts the two minutes from the end of that cycle. For the LRU without Green Tea, in {{< single "timer.a.nogreen" >}} GC cycles only one of the two background mark workers was effectively marking (CPU time equals wall-clock time). With Green Tea, both always were, and this accounts for most of the wall-clock gain. This is an observation from this benchmark, not a general property of Green Tea: I haven't yet worked out why only one worker runs without it.

{{< timer-table >}}

## Growth with cache size

`runtime.GC()` calls, marking CPU, one run per point. For the variants with pointers, the cost grows with the number of entries. For the flat map, it barely changes.

{{< chart scaling >}}

## All configurations

{{< all-table >}}

</details>
