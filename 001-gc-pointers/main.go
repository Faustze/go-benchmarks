package main

import (
	"flag"    // флаги командной строки
	"fmt"     // печать
	"os"      // файлы, stderr, выход
	"runtime" // GC, MemStats и KeepAlive
	"strconv" // число → строка
	"time"    // время
)

const (
	fullTime  = time.Duration(11) * time.Minute // простой в полном прогоне
	checkTime = time.Duration(5) * time.Minute  // простой в smoke check
)

// варианты раскладки: одни и те же ReadState, разное число указателей на запись
var variants = map[string]string{
	"a":  "map[string]*entry + двусвязный список (LRU), 6 указателей",
	"p":  "map[uint64]*ReadState, 1 указатель",
	"b2": "map[string]ReadState, 1 указатель (байты ключа)",
	"b":  "map[uint64]ReadState, 0 указателей",
}

func main() {
	variant := flag.String("variant", "a", "a | p | b2 | b")                           // *string
	records := flag.Int("n", 10_000_000, "число записей")                              // *int
	idle := flag.Duration("idle", fullTime, "простой (smoke: "+checkTime.String()+")") // *time.Duration
	mode := flag.String("mode", "timer", "timer — ждать сборок по таймеру, forced — звать runtime.GC()")
	forced := flag.Int("forced", 8, "сколько раз звать runtime.GC() в режиме forced")
	flag.Parse() // читаем аргументы в переменные выше

	if _, ok := variants[*variant]; !ok { // неправильный вызов
		fmt.Fprintf(os.Stderr, "неизвестный вариант %q, нужен a, p, b2 или b\n", *variant) // stderr: терминал или файл из 2>
		os.Exit(2)                                                                         // 2 — «неправильно вызвали»
	}
	if *mode != "timer" && *mode != "forced" {
		fmt.Fprintf(os.Stderr, "неизвестный режим %q, нужен timer или forced\n", *mode)
		os.Exit(2)
	}

	A := make(map[string]*entry)     // вариант a: строка → указатель
	P := make(map[uint64]*ReadState) // вариант p: число → указатель
	B2 := make(map[string]ReadState) // вариант b2: строка → значение
	B := make(map[uint64]ReadState)  // вариант b: число → значение

	start := time.Now() // старт заливки

	// 1. заполняем кэш
	switch *variant {
	case "a":
		var tail *entry                 // последний элемент списка; пока nil
		for i := 0; i < *records; i++ { // i = 0, 1, 2 … n-1
			key := strconv.Itoa(i) // 5 → "5" (объект в куче №1)
			e := &entry{           // новый entry, берём адрес (объект №2)
				key:   key,                                                              // указатель 1: на байты строки
				value: &ReadState{LastReadID: uint64(i), MentionCount: uint32(i % 100)}, // указатель 2: на ReadState (объект №3)
				prev:  tail,                                                             // указатель 3: на предыдущий
			}
			if tail != nil { // у первого элемента предыдущего нет
				tail.next = e // указатель 4: предыдущий → текущий
			}
			tail = e   // теперь хвост — текущий
			A[key] = e // + указатели 5 и 6: ключ и значение в map
		}
	case "p":
		for i := 0; i < *records; i++ {
			P[uint64(i)] = &ReadState{LastReadID: uint64(i), MentionCount: uint32(i % 100)} // отдельный объект, в map лежит указатель
		}
	case "b2":
		for i := 0; i < *records; i++ {
			B2[strconv.Itoa(i)] = ReadState{LastReadID: uint64(i), MentionCount: uint32(i % 100)} // значение в map, но ключ-строка несёт указатель
		}
	case "b":
		for i := 0; i < *records; i++ { // i = 0, 1, 2 … n-1
			B[uint64(i)] = ReadState{LastReadID: uint64(i), MentionCount: uint32(i % 100)} // значение копируется в map, указателей 0
		}
	}

	fill := time.Since(start) // сколько заняла заливка

	runtime.GC()            // первый GC: без него forced GC раз в 2 мин не стартует
	time.Sleep(time.Second) // даём gctrace допечатать строку до маркера

	var ms runtime.MemStats // сколько объектов и байт живёт в куче после заливки
	runtime.ReadMemStats(&ms)

	// 2. маркер: всё ниже — GC на простое
	fmt.Fprintf(os.Stderr, "=== FILLED variant=%s n=%d mode=%s fill=%v idle=%v objects=%d heap=%d ===\n",
		*variant, *records, *mode, fill.Round(time.Millisecond), *idle, ms.HeapObjects, ms.HeapAlloc)

	// 3. простой: аллокаций нет
	switch *mode {
	case "timer":
		time.Sleep(*idle) // sysmon сам вызывает GC раз в 2 минуты
	case "forced":
		for i := 0; i < *forced; i++ {
			runtime.GC()                       // сборка того же неизменного хипа
			time.Sleep(200 * time.Millisecond) // gctrace успевает допечатать
		}
	}

	// 4. обращаемся к кэшу после простоя — значит, он жив всё это время
	last := *records - 1 // индекс последней записи
	var size int
	var lastID uint64
	switch *variant {
	case "a":
		e := A[strconv.Itoa(last)] // ищем последнюю запись
		if e == nil {              // нет её только при -n 0
			fmt.Fprintln(os.Stderr, "=== END: кэш пуст ===") // пишем и выходим
			return
		}
		size, lastID = len(A), e.value.LastReadID
	case "p":
		s := P[uint64(last)]
		if s == nil {
			fmt.Fprintln(os.Stderr, "=== END: кэш пуст ===")
			return
		}
		size, lastID = len(P), s.LastReadID
	case "b2":
		size, lastID = len(B2), B2[strconv.Itoa(last)].LastReadID // нет ключа → нулевой ReadState, не паника
	case "b":
		size, lastID = len(B), B[uint64(last)].LastReadID
	}
	fmt.Fprintf(os.Stderr, "=== END len=%d last.LastReadID=%d ===\n", size, lastID) // размер и последняя запись
	runtime.KeepAlive(A)                                                            // явно: мапы нужны до этой строки
	runtime.KeepAlive(P)
	runtime.KeepAlive(B2)
	runtime.KeepAlive(B)
}

type ReadState struct { // состояние чтения; без указателей
	LastReadID   uint64 // 8 байт
	MentionCount uint32 // 4 байта (+4 выравнивание)
}

type entry struct { // узел двусвязного списка; 4 указателя
	key   string     // строка = указатель + длина
	value *ReadState // на данные
	prev  *entry     // на предыдущий
	next  *entry     // на следующий
}
