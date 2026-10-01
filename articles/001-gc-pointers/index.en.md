+++
title = "What a pointer costs"
description = "A discord case on Go 1.27.1: how much time the garbage collector spends on a cache of 10M entries with and without pointers, and how to store such data more cheaply."
date = 2026-09-30
go = "1.27.1"
hardware = "i7-6700 · GOMAXPROCS=8"
translation = "machine"
source_hash = "b62d926524416ae0"
+++

The same 10M records in a Go service's memory can cost the [garbage collector](#g-gc) about a millisecond per cycle, or almost a second. The only difference is how the data is stored: whether their types contain pointers or not. This page explains why that happens and how to store long-lived data so the collector has less work to do. First some context, then examples, what to do and how to check your own service, with charts at the end. Unfamiliar terms are collected in the [glossary](#glossary) at the bottom of the page.

## Some context

### How the garbage collector finds live objects

Everything the program creates via `new`, `make`, `&T{}`, slices, strings and maps usually lives on the [heap](#g-heap). In Go you don't free this memory by hand: from time to time the garbage collector (GC) runs and finds out on its own what is still in use.

It does so like this. It takes the [roots](#g-roots): global variables and variables on goroutine stacks. From them it follows every pointer, marks the object it reached as [live](#g-live), and then looks at that object's pointer fields. This is called [marking (mark/scan)](#g-mark). Everything the collector did not reach is considered garbage and is freed.

Hence the main point: **marking work grows with the number of live objects and the pointers between them**. If a type has no pointers at all, the collector marks such an object as live and does not look inside (such objects are called [noscan](#g-noscan)).

When a collection starts. Usually when the heap has grown (this is governed by [`GOGC`](#g-gogc)). But there is also a [timer-triggered GC](#g-timer): if there has been no collection for two minutes, the runtime starts one itself. A service that hardly creates any new objects gets only such collections.

### What an LRU cache is

A cache stores results that are expensive to obtain again, for example database responses. Memory is not infinite, so the cache has a limit, and when it is full something has to be thrown out. [LRU](#g-lru) (least recently used) evicts the entry that has gone unused the longest.

A classic Go implementation: a map from key to node plus a [doubly linked list](#g-list) of nodes. On every access the node is moved to the head of the list, and the node at the tail is evicted:

```go
type entry struct {
	key        string     // the key, so the entry can be deleted from the map on eviction
	value      *ReadState // the data itself
	prev, next *entry     // neighbors in the list
}

cache := map[string]*entry{}
```

Convenient and fast, but each entry carries several pointers: from the map to the node, from the node to its neighbors, to the data and to the key bytes.

### The discord case

In 2020 discord [rewrote the Read States service from Go to Rust](https://discord.com/blog/why-discord-is-switching-from-go-to-rust). The service kept an LRU cache of millions of entries in memory and created almost nothing anew. So collections came to it only from the timer, once every two minutes, and each one walked the entire cache. On the graphs this showed up as a CPU and response latency spike every two minutes.

The timer is still in the Go runtime. I checked the same mechanics on Go 1.27.1.

## Four examples and why they are the way they are

All examples hold the same 10M `ReadState` records: two numeric fields, 16 bytes, no pointers.

```go
type ReadState struct {
	LastReadID   uint64
	MentionCount uint32
}
```

Only the storage method changes. Each following example removes one more source of pointers, so that you can see what each of them costs:

| Example | How it is stored | What changed and why | Pointers per record | Objects on the heap |
|---|---|---|---|---|
| A · LRU as in discord | `map[string]*entry` + doubly linked list | Starting point: reproduces the discord case | 6 | {{< num "timer.a.green.objects" count >}} |
| P · map of pointers | `map[uint64]*ReadState` | Removed the list and the string key, but the record still lives behind a pointer. The most common way to keep data in memory: a map-based "repository". The question: what does a single `*` per record cost | 1 | {{< num "timer.p.green.objects" count >}} |
| B2 · string key | `map[string]ReadState` | Removed the `*`, the record lies directly in the map, but the key is still a string. The question: if there is not a single `*` in the code, does that mean there are no pointers | 1, in the key | {{< num "timer.b2.green.objects" count >}} |
| B · flat map | `map[uint64]ReadState` | Not a single pointer. Lower bound: what a collection costs when there is no need to look inside | 0 | {{< num "timer.b.green.objects" count >}} |

The data is deliberately identical: this way the difference in cost is explained only by the storage method, not by volume. B2 has half as many objects as keys: the runtime puts the bytes of two short strings into one block, this is done by the [tiny allocator](#g-tiny). P and B also weigh almost the same ({{< num "timer.p.green.live_mb" n0 >}} and {{< num "timer.b.green.live_mb" n0 >}} MB), so they show well that the collector does not pay per megabyte.

Conditions: a [synthetic](#g-synthetic) cache without requests and without [eviction](#g-eviction), filled once, after which the program does nothing for 11 minutes, and only timer-triggered collections count. i7-6700, Linux, [`GOMAXPROCS=8`](#g-gomaxprocs), three runs per example.

{{< kpis >}}

**What the numbers mean.** [Mark/scan wall-clock time](#g-clock): how long background marking lasts, while the program keeps running. Mark CPU: how much CPU time marking consumed in total across all cores. [STW](#g-stw): how long the program actually stood still, here no more than {{< stw >}} per collection. Live heap: how much memory is in use after marking. The other phases of a collection are short, so below "collection" almost everywhere means its marking.

## What to do

This applies only to data that lives long and exists in millions of instances: caches, [in-memory indexes](#g-index), reference tables. Temporary objects created for a single request are already dead by the time of a collection, and the collector does not walk them. The effect below was measured on a timer-triggered GC at 10M records ([median](#g-median) mark/scan wall-clock time).

### Number as key, record as value

`map[string]*Entry` → `map[uint64]Entry`

The string key becomes a unique numeric id, and the record is stored as a value with no pointers inside. A string [hash](#g-hash) instead of an id is suitable only if [collisions](#g-collision) are ruled out or handled. In the benchmark, "before" corresponds to the LRU as in discord: such a map plus a doubly linked list. Effect: {{< num "timer.a.green.mark_clock_ms" ms >}} → {{< num "timer.b.green.mark_clock_ms" ms >}} per collection.

### Value instead of pointer

`map[K]*T`, `[]*T` → `map[K]T`, `[]T`

One object and one pointer fewer per record. It works if T is small, has no pointers itself, and the record does not need a separate [identity](#g-identity): nobody holds a `*T`, and the record is replaced as a whole via `m[k] = v`. Cost: T is copied on every read and update, which is expensive for a large T. Effect: {{< num "timer.p.green.mark_clock_ms" ms >}} → {{< num "timer.b.green.mark_clock_ms" ms >}} per collection.

### Key without a string

`map[string]T` → `map[uint64]T`

A string holds a pointer to its bytes. A single string key is enough for the collector to walk the entire map, even if the values have no pointers. Effect: {{< num "timer.b2.green.mark_clock_ms" ms >}} → {{< num "timer.b.green.mark_clock_ms" ms >}} per collection.

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

`time.Time` hides a `*Location`, and `string` and `any` also contain pointers. One such field makes the whole struct scannable. `int64` ([Unix time](#g-unix) in nanoseconds) is suitable if you only need a point in time: the time zone and the [monotonic clock](#g-mono) are lost. Not benchmarked separately, the mechanism is the same.

### Links via indexes

`prev, next *entry` → `prev, next int32 // indexes into []entry`

Lists and trees (an LRU is also a list) can be kept in a single slice of nodes, and instead of pointers to neighbors you store their positions in that slice. In the benchmarked LRU, `prev` and `next` account for 2 of the 6 pointers per record. Cost: you have to manage freed slots yourself, for example with a [free list](#g-freelist). Not benchmarked separately.

### Strings in one buffer

`[]string` → `buf []byte` and `span []struct{ off, n uint32 }`

The bytes of all strings lie in one `[]byte`, and the record stores an offset and a length. This is how [bigcache and freecache](#g-bigcache) are built. Cost: deleted strings leave holes, and the buffer has to be compacted. Not benchmarked separately.

> **What does not help.** Ordinary [`GOGC` and `GOMEMLIMIT`](#g-gogc) values change how often a collection comes because of heap growth. They do not cancel the timer-triggered GC every two minutes, and each such collection walks all live objects. `GOGC=off` turns off the timer too. If `GOMEMLIMIT` is set at the same time, collections come when the heap reaches the limit; without a limit the heap grows with no collections at all. This is from the runtime code and the [GC guide](https://go.dev/doc/gc-guide), not benchmarked separately.

## How to estimate the cost in your own service

In this benchmark, the cost of marking was best explained by the number of live objects: on the i7-6700 it came to about 100 ns CPU per object. This is a figure for my hardware and my structs (at 10M records across examples and modes it came to {{< nsrange >}} ns), not a Go constant. The cost is also affected by the size of the scanned part of an object and how objects are scattered across memory, and on another CPU the figure will be different. As a rough estimate: a million long-lived objects on the i7-6700 cost on the order of 100 ms CPU per collection.

**1. How many objects live on the heap.** A metric from the standard library [`runtime/metrics`](#g-metrics):

```go
s := []metrics.Sample{{Name: "/gc/heap/objects:objects"}}
metrics.Read(s) // import "runtime/metrics"
n := s[0].Value.Uint64()
fmt.Printf("~%v CPU per GC\n", time.Duration(n)*100) // ~100 ns per object
```

**2. Where they were created.** A [heap profile](#g-pprof) by the number of live objects; `top` will show the functions that created the most live objects. The profile will not show who retains them, that is found from the code:

```sh
go tool pprof -sample_index=inuse_objects http://localhost:6060/debug/pprof/heap
```

**3. How long a collection actually lasts.** Run the service with [`GODEBUG=gctrace=1`](#g-gctrace): the runtime will print a line for each collection. The second term in `ms clock` shows mark/scan wall-clock time.

**4. Whether a timer-triggered collection came.** Before such a collection gctrace prints a `GC forced` line:

```sh
GODEBUG=gctrace=1 ./app 2>&1 | grep -A1 "GC forced"
gc 15 @128.984s 0%: 0.047+1001+0.004 ms clock, ...
```

## One collection, four examples

Median mark/scan wall-clock time for timer-triggered collections at full idle. The data is identical, only the number of pointers per record changes. In parentheses is the spread across {{< num "timer.a.green.gcs" n0 >}} collections. The scale is [logarithmic](#g-log); on a linear one the flat map is not visible at all.

{{< chart hero >}}

## The cost is explained by the number of objects

20 runs with `runtime.GC()` from 100K to 10M records, one per point, so this is an observation on a single [workload](#g-workload), no law can be derived from it. By memory volume the points are spread over three orders of magnitude: the flat map at {{< num "forced.b.green.live_mb" n0 >}} MB costs {{< num "forced.b.green.mark_cpu_ms" ms >}} CPU, the map of pointers at {{< num "forced.p.green.live_mb" n0 >}} MB costs {{< num "forced.p.green.mark_cpu_ms" ms >}}. By number of objects all examples lie close to a single line.

{{< chart objects >}}

## The discord case: a collection every two minutes

The program fills the cache and does nothing for 11 minutes. Collections still come every two minutes, and each time the LRU occupies two logical CPUs out of eight for about a second: for marking the runtime allocates [background mark workers](#g-workers) at 25% of `GOMAXPROCS`. Under load this second would share CPU with requests, and goroutines that create objects at that time could be subject to [mark assist](#g-assist) and help with marking themselves.

{{< chart timeline >}}

## Where pointers hide in Go

The collector goes inside an object if its type has at least one pointer field. A type without pointers (numbers, `bool`, arrays of them, structs of such fields) it skips entirely.

| Type | Pointers | What is inside |
|---|---|---|
| `int, float64, bool, [16]byte` | no | Numbers and arrays of numbers. The collector will mark the object live and will not go inside. |
| `struct{ ID uint64; N uint32 }` | no | A struct of numbers is also noscan. This is exactly how ReadState lies in the flat map. |
| `string` | yes | A pointer to the bytes plus a length. A `map[string]T` key makes the collector walk the entire map: at idle it is about {{< ratio "timer.b2.green.mark_clock_ms" "timer.b.green.mark_clock_ms" r10 >}} times more expensive than `map[uint64]T` with the same values. |
| `[]T` | yes | A pointer to an array, a length and a capacity. A slice of numbers: one pointer to the slice, the array itself is noscan. |
| `*T, map, chan, func` | yes | Each of them is a pointer. `map[K]V` is scanned only if K or V contain pointers. |
| `interface{}, any, error` | yes | Two words: a type and a pointer to the value. An `any` field in a struct makes it scannable. |
| `time.Time` | yes | Inside is `loc *Location`. A struct with a `time.Time` field is no longer noscan. For a cache you can store Unix time in an `int64` if you do not need the time zone and monotonic time. |
| struct with one pointer | yes | A single pointer field is enough for the collector to read the whole object and follow the reference. |

<details>
<summary>Details: methodology and all numbers</summary>

## How I benchmarked

Two modes. **Timer**: 10M records, 11 minutes of idle with no allocations, collections are triggered only by the runtime timer (`forcegcperiod`); three runs per example. **`runtime.GC()`**: 8 calls in a row at sizes from 100K to 10M, one run per configuration. Figures from `GODEBUG=gctrace=1`, the first collection after filling does not count. All on the default collector in Go 1.27.1, the same one any `go build` gets.

The interval between collections equals 120 s plus the duration of the previous collection: the runtime counts two minutes from its end.

{{< timer-table >}}

## Growth with cache size

`runtime.GC()` calls, marking CPU, one run per point. For the examples with pointers the cost grows with the number of records, for the flat map it barely changes.

{{< chart scaling >}}

## All configurations

{{< all-table >}}

</details>

## Glossary

- <a id="g-gc"></a>**Garbage collector (GC).** The part of the Go runtime that finds and frees memory the program no longer uses, on its own.
- <a id="g-heap"></a>**Heap.** The memory area for objects that live longer than a single function call. This is what the collector deals with. Variables the compiler managed to keep on the stack do not load the collector.
- <a id="g-roots"></a>**Roots.** What the collector starts its walk from: global variables and variables on goroutine stacks.
- <a id="g-live"></a>**Live object.** An object that can be reached from the roots via pointers. It must not be freed.
- <a id="g-mark"></a>**Marking (mark/scan).** The main phase of a collection: the collector walks live objects via pointers, marks them (mark) and reads their pointer fields (scan).
- <a id="g-noscan"></a>**noscan.** An object whose type has no pointers. The collector marks it live but does not look inside, so such an object costs almost nothing.
- <a id="g-stw"></a>**STW (stop the world).** Short pauses at the start and end of a collection, when all goroutines are stopped. Marking itself runs concurrently with the program.
- <a id="g-clock"></a>**Wall-clock and CPU.** Wall-clock: how much real time has passed. CPU: how much CPU time was spent on all cores together. Two cores running for a second of wall-clock time give two seconds of CPU.
- <a id="g-workers"></a>**Background GC workers.** Threads that do the marking while the program runs. The runtime gives them 25% of `GOMAXPROCS`: at 8 that is two logical CPUs.
- <a id="g-assist"></a>**Mark assist.** If a goroutine actively creates objects during marking, the runtime makes it do some marking work itself. For a request this looks like extra latency.
- <a id="g-gomaxprocs"></a>**GOMAXPROCS.** How many logical CPUs a Go program may occupy at the same time. By default equal to the number of logical CPUs of the machine.
- <a id="g-timer"></a>**Timer-triggered GC (`forcegcperiod`).** If there has been no collection for two minutes, the runtime starts one itself, even if memory is not growing. In gctrace a `GC forced` line precedes it.
- <a id="g-lru"></a>**LRU cache (least recently used).** A cache with a bounded size: when full, it evicts the entry that has gone unused the longest.
- <a id="g-list"></a>**Doubly linked list.** A chain of nodes where each stores pointers to the previous and next ones. A node can be removed or moved in constant time, which is why an LRU keeps the access order on it.
- <a id="g-eviction"></a>**Eviction.** Removing an entry from the cache when it is full.
- <a id="g-index"></a>**In-memory index.** A structure in the service's memory used to look up data quickly: for example, a map from user id to their settings, to avoid going to the database.
- <a id="g-workload"></a>**Workload.** A specific load: what data, how much, and what is done with it. A conclusion drawn on one workload may not hold on another.
- <a id="g-synthetic"></a>**Synthetic benchmark.** A program written only for benchmarking. It reproduces the mechanics of interest, but not a real service with its requests.
- <a id="g-tiny"></a>**Tiny allocator.** A runtime mechanism that packs several very small objects without pointers into one 16-byte block. This is why example B2 has half as many objects as keys: the bytes of two short strings lie in one block.
- <a id="g-hash"></a>**Hash.** A number a function computes from a string. Identical strings always give the same hash.
- <a id="g-collision"></a>**Collision.** Two different strings gave the same hash. If the map key is a hash, on a collision one record will silently overwrite the other.
- <a id="g-identity"></a>**Record identity.** When it matters that it is the very same object in memory, not a copy of it: for example, someone holds a `*T` and expects to see changes. If the record is stored by value, there is no such reference, only copies.
- <a id="g-unix"></a>**Unix time.** Time as a single number: how many seconds (or nanoseconds) have passed since 1 January 1970 UTC. In Go: `t.UnixNano()` and back `time.Unix(0, n)`.
- <a id="g-mono"></a>**Monotonic clock.** A clock reading that only increases, even if the system time was adjusted. `time.Now()` stores it inside `time.Time` so that `time.Since` computes durations correctly. It is not preserved in an `int64`.
- <a id="g-freelist"></a>**Free list.** A list of indexes of freed slice cells, so that new records take them instead of growing the slice.
- <a id="g-bigcache"></a>**bigcache and freecache.** Popular Go cache libraries built so as not to load the collector: records lie in large byte buffers rather than in millions of separate objects.
- <a id="g-gogc"></a>**GOGC and GOMEMLIMIT.** Collector settings. `GOGC` (100 by default): by what percentage the heap may grow before the next collection. `GOMEMLIMIT`: a soft memory ceiling near which the collector starts working more often.
- <a id="g-gctrace"></a>**gctrace.** Runtime debug output, enabled via `GODEBUG=gctrace=1`: one line per collection with its duration, CPU and heap size.
- <a id="g-pprof"></a>**pprof and heap profile.** The profiler built into Go. A heap profile shows which functions created the objects that currently live on the heap.
- <a id="g-metrics"></a>**runtime/metrics.** A standard library package that exposes runtime metrics: the number of objects on the heap, its size, the number of collections and more.
- <a id="g-median"></a>**Median.** The middle value when all measurements are sorted. Unlike the mean, a single outlier barely shifts it.
- <a id="g-log"></a>**Logarithmic scale.** Each division of the scale is 10 times larger than the previous one. Needed when numbers on one chart differ by hundreds of times.
