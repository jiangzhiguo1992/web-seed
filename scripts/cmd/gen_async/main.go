// gen_async 把 capabilities/events 契约投影为 worker 异步任务清单。
//
// 用法（在 scripts/cmd 下）：
//
//	go run ./gen_async -root ../..
//	go run ./gen_async -root ../.. -check
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
	"gopkg.in/yaml.v3"
)

const targetRel = "contracts/generated/async/manifest.yaml"

type retryPolicy struct {
	MaxAttempts int    `yaml:"max_attempts"`
	Backoff     string `yaml:"backoff"`
	DeadLetter  bool   `yaml:"dead_letter"`
}

type concurrencyPolicy struct {
	Key string `yaml:"key"`
	TTL string `yaml:"ttl,omitempty"`
}

type subscribe struct {
	ID          string             `yaml:"id"`
	Name        string             `yaml:"name"`
	Service     string             `yaml:"service"`
	Retry       *retryPolicy       `yaml:"retry,omitempty"`
	Concurrency *concurrencyPolicy `yaml:"concurrency,omitempty"`
}

type topic struct {
	ID         string `yaml:"id"`
	Name       string `yaml:"name"`
	Version    int    `yaml:"version"`
	Idempotent bool   `yaml:"idempotent"`
	Publish    struct {
		EmittedBy []string `yaml:"emitted_by"`
	} `yaml:"publish"`
	Subscribe []subscribe `yaml:"subscribe"`
}

type cron struct {
	ID          string             `yaml:"id"`
	Name        string             `yaml:"name"`
	Service     string             `yaml:"service"`
	Schedule    string             `yaml:"schedule"`
	Overlap     string             `yaml:"overlap"`
	Retry       *retryPolicy       `yaml:"retry,omitempty"`
	Concurrency *concurrencyPolicy `yaml:"concurrency,omitempty"`
}

type dispatch struct {
	ID          string             `yaml:"id"`
	Name        string             `yaml:"name"`
	Service     string             `yaml:"service"`
	EmittedBy   []string           `yaml:"emitted_by"`
	Delay       string             `yaml:"delay,omitempty"`
	Timeout     string             `yaml:"timeout,omitempty"`
	Retry       *retryPolicy       `yaml:"retry,omitempty"`
	Concurrency *concurrencyPolicy `yaml:"concurrency,omitempty"`
}

type eventsDoc struct {
	Topics     []topic    `yaml:"topics"`
	Crons      []cron     `yaml:"crons"`
	Dispatches []dispatch `yaml:"dispatches"`
}

type topicEntry struct {
	ID          string            `yaml:"id"`
	Name        string            `yaml:"name"`
	Version     int               `yaml:"version"`
	Idempotent  bool              `yaml:"idempotent"`
	EmittedBy   []string          `yaml:"emitted_by"`
	Subscribers []subscriberEntry `yaml:"subscribers,omitempty"`
}

type subscriberEntry struct {
	ID          string             `yaml:"id"`
	Name        string             `yaml:"name"`
	Service     string             `yaml:"service"`
	Retry       *retryPolicy       `yaml:"retry,omitempty"`
	Concurrency *concurrencyPolicy `yaml:"concurrency,omitempty"`
}

type cronEntry struct {
	ID          string             `yaml:"id"`
	Name        string             `yaml:"name"`
	Service     string             `yaml:"service"`
	Schedule    string             `yaml:"schedule"`
	Overlap     string             `yaml:"overlap"`
	Retry       *retryPolicy       `yaml:"retry,omitempty"`
	Concurrency *concurrencyPolicy `yaml:"concurrency,omitempty"`
}

type dispatchEntry struct {
	ID          string             `yaml:"id"`
	Name        string             `yaml:"name"`
	Service     string             `yaml:"service"`
	EmittedBy   []string           `yaml:"emitted_by"`
	Delay       string             `yaml:"delay,omitempty"`
	Timeout     string             `yaml:"timeout,omitempty"`
	Retry       *retryPolicy       `yaml:"retry,omitempty"`
	Concurrency *concurrencyPolicy `yaml:"concurrency,omitempty"`
}

type manifest struct {
	SchemaVersion int             `yaml:"schema_version"`
	GeneratedFrom string          `yaml:"generated_from"`
	Topics        []topicEntry    `yaml:"topics,omitempty"`
	Crons         []cronEntry     `yaml:"crons,omitempty"`
	Dispatches    []dispatchEntry `yaml:"dispatches,omitempty"`
}

func main() {
	root := flag.String("root", "../..", "repo root")
	check := flag.Bool("check", false, "only verify manifest is up to date")
	flag.Parse()
	abs, err := filepath.Abs(*root)
	if err != nil {
		fail(err)
	}
	got, err := build(abs)
	if err != nil {
		fail(err)
	}
	target := filepath.Join(abs, targetRel)
	if *check {
		if err := checkManifest(target, got); err != nil {
			fail(err)
		}
		fmt.Println("async: OK")
		return
	}
	if err := writeManifest(target, got); err != nil {
		fail(err)
	}
	fmt.Println("generated", target)
}

func checkManifest(target string, got []byte) error {
	existing, err := os.ReadFile(target)
	if err != nil {
		return fmt.Errorf("%s: %w (run make gen-async)", target, err)
	}
	if !bytes.Equal(existing, got) {
		return fmt.Errorf("%s is not up to date; run make gen-async", target)
	}
	return nil
}

func writeManifest(target string, got []byte) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, got, 0o644)
}

func build(root string) ([]byte, error) {
	schema, _, err := contractspec.LoadSchemaByType(root, "cap.events")
	if err != nil {
		return nil, err
	}
	files, err := filepath.Glob(filepath.Join(root, schema.Targets.Dir, schema.Targets.Glob))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	docs := make([]eventsDoc, 0, len(files))
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		var doc eventsDoc
		if err := yaml.Unmarshal(b, &doc); err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		docs = append(docs, doc)
	}
	return marshal(buildManifest(docs))
}

func buildManifest(docs []eventsDoc) manifest {
	m := manifest{SchemaVersion: 1, GeneratedFrom: "contracts/capabilities/events"}
	for _, doc := range docs {
		for _, t := range doc.Topics {
			emitted := append([]string{}, t.Publish.EmittedBy...)
			sort.Strings(emitted)
			entry := topicEntry{
				ID: t.ID, Name: t.Name, Version: t.Version,
				Idempotent: t.Idempotent, EmittedBy: emitted,
			}
			for _, s := range t.Subscribe {
				entry.Subscribers = append(entry.Subscribers, subscriberEntry{
					ID: s.ID, Name: s.Name, Service: s.Service,
					Retry: s.Retry, Concurrency: s.Concurrency,
				})
			}
			sort.Slice(entry.Subscribers, func(i, j int) bool { return entry.Subscribers[i].ID < entry.Subscribers[j].ID })
			m.Topics = append(m.Topics, entry)
		}
		for _, c := range doc.Crons {
			m.Crons = append(m.Crons, cronEntry{
				ID: c.ID, Name: c.Name, Service: c.Service, Schedule: c.Schedule,
				Overlap: c.Overlap, Retry: c.Retry, Concurrency: c.Concurrency,
			})
		}
		for _, d := range doc.Dispatches {
			m.Dispatches = append(m.Dispatches, dispatchEntry{
				ID: d.ID, Name: d.Name, Service: d.Service,
				EmittedBy: append([]string{}, d.EmittedBy...),
				Delay:     d.Delay, Timeout: d.Timeout, Retry: d.Retry, Concurrency: d.Concurrency,
			})
		}
	}
	sort.Slice(m.Topics, func(i, j int) bool { return m.Topics[i].ID < m.Topics[j].ID })
	sort.Slice(m.Crons, func(i, j int) bool { return m.Crons[i].ID < m.Crons[j].ID })
	sort.Slice(m.Dispatches, func(i, j int) bool { return m.Dispatches[i].ID < m.Dispatches[j].ID })
	return m
}

func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gen-async:", err)
	os.Exit(1)
}
