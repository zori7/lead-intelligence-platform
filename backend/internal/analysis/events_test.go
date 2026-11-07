package analysis

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"leadintel/backend/internal/api"
	"leadintel/backend/internal/apitest"
)

// ---- Broker -------------------------------------------------------------------------

func recv(t *testing.T, ch <-chan StreamEvent) (StreamEvent, bool) {
	t.Helper()
	select {
	case ev, ok := <-ch:
		return ev, ok
	case <-time.After(time.Second):
		t.Fatal("no event within 1s")
		return StreamEvent{}, false
	}
}

func TestBrokerFanOut(t *testing.T) {
	b := NewBroker()
	job, other := uuid.New(), uuid.New()
	a, unsubA := b.Subscribe(job)
	defer unsubA()
	c, unsubC := b.Subscribe(job)
	defer unsubC()
	o, unsubO := b.Subscribe(other)
	defer unsubO()

	b.Publish(job, StreamEvent{Name: EventProgress})
	for _, ch := range []<-chan StreamEvent{a, c} {
		if ev, ok := recv(t, ch); !ok || ev.Name != EventProgress {
			t.Fatalf("got %+v ok=%v", ev, ok)
		}
	}
	select {
	case ev := <-o:
		t.Fatalf("other job's subscriber got %+v", ev)
	default:
	}
}

func TestBrokerUnsubscribe(t *testing.T) {
	b := NewBroker()
	job := uuid.New()
	ch, unsub := b.Subscribe(job)
	unsub()
	unsub() // idempotent
	if _, ok := recv(t, ch); ok {
		t.Fatal("channel still open after unsubscribe")
	}
	b.Publish(job, StreamEvent{Name: EventProgress}) // must not panic on a closed channel
	if len(b.subs) != 0 {
		t.Fatalf("subscriber not removed: %v", b.subs)
	}
}

func TestBrokerDropsSlowSubscriber(t *testing.T) {
	b := NewBroker()
	job := uuid.New()
	slow, unsubSlow := b.Subscribe(job)
	defer unsubSlow()
	fast, unsubFast := b.Subscribe(job)
	defer unsubFast()

	done := make(chan struct{})
	go func() { // keep the fast subscriber drained
		defer close(done)
		for range fast {
		}
	}()
	for range subscriberBuffer + 1 {
		b.Publish(job, StreamEvent{Name: EventProgress}) // never blocks
	}

	n := 0
	for range slow {
		n++
	}
	if n != subscriberBuffer {
		t.Fatalf("slow subscriber got %d buffered events before being dropped, want %d", n, subscriberBuffer)
	}
	unsubFast()
	<-done
}

func TestBrokerClose(t *testing.T) {
	b := NewBroker()
	ch, unsub := b.Subscribe(uuid.New())
	defer unsub()
	b.Close()
	if _, ok := recv(t, ch); ok {
		t.Fatal("open stream survived Close")
	}
	late, _ := b.Subscribe(uuid.New())
	if _, ok := recv(t, late); ok {
		t.Fatal("subscription accepted after Close")
	}
}

// ---- SSE handler ----------------------------------------------------------------------

type sseEvent struct {
	name, data string
}

// readEvents parses an SSE body into events (comments are skipped).
func readEvents(t *testing.T, r io.Reader, stopAfter int) []sseEvent {
	t.Helper()
	var (
		out []sseEvent
		cur sseEvent
	)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if cur.name != "" {
				out = append(out, cur)
				if len(out) == stopAfter {
					return out
				}
			}
			cur = sseEvent{}
		case strings.HasPrefix(line, ":"):
		case strings.HasPrefix(line, "event: "):
			cur.name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			cur.data = strings.TrimPrefix(line, "data: ")
		default:
			t.Fatalf("malformed SSE line %q", line)
		}
	}
	return out
}

func names(evs []sseEvent) []string {
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i] = e.name
	}
	return out
}

func TestEventsForFinishedJob(t *testing.T) {
	l := sampleLead()
	failed := queuedJob()
	failed.Status, failed.Error = api.JobFailed, &JobError{Code: "timeout", Message: "Retry.", Retryable: true}

	for _, tc := range []struct {
		job  Job
		want string
	}{
		{succeededJob(l.ID), EventCompleted},
		{failed, EventFailed},
	} {
		t.Run(tc.want, func(t *testing.T) {
			h := NewHandler(&fakeJobs{job: tc.job}, fakeLeads{l}, fakeSettings{configured()}, &fakeRunner{}, NewBroker())
			rec := httptest.NewRecorder()
			// The handler must return on its own: a terminal job closes the stream.
			newRouter(h).ServeHTTP(rec, httptest.NewRequest("GET", "/api/analyses/"+tc.job.ID.String()+"/events", nil))

			if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/event-stream" ||
				rec.Header().Get("Cache-Control") != "no-cache" || rec.Header().Get("X-Accel-Buffering") != "no" {
				t.Fatalf("status %d, headers %v", rec.Code, rec.Header())
			}
			evs := readEvents(t, rec.Body, 0)
			if got := names(evs); len(got) != 2 || got[0] != EventSnapshot || got[1] != tc.want {
				t.Fatalf("events %v, want [snapshot %s]", got, tc.want)
			}
			for _, e := range evs {
				var body any
				if err := json.Unmarshal([]byte(e.data), &body); err != nil {
					t.Fatalf("%s data is not JSON: %v", e.name, err)
				}
				apitest.CheckSchema(t, "AnalysisJob", body)
			}
		})
	}
}

func TestEventsUnknownJob(t *testing.T) {
	h := NewHandler(&fakeJobs{job: queuedJob()}, fakeLeads{sampleLead()}, fakeSettings{configured()}, &fakeRunner{}, NewBroker())
	rec := httptest.NewRecorder()
	newRouter(h).ServeHTTP(rec, httptest.NewRequest("GET", "/api/analyses/"+uuid.NewString()+"/events", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
	apitest.CheckResponse(t, "GET", "/analyses/{id}/events", rec)
}

func TestEventsLiveStream(t *testing.T) {
	job := queuedJob()
	job.Status = api.JobRunning
	broker := NewBroker()
	h := NewHandler(&fakeJobs{job: job}, fakeLeads{sampleLead()}, fakeSettings{configured()}, &fakeRunner{}, broker)
	h.ping = 10 * time.Millisecond
	srv := httptest.NewServer(newRouter(h))
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/analyses/" + job.ID.String() + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body := bufio.NewReader(res.Body)

	// The snapshot arrives first; the subscription already exists by then.
	if evs := readEvents(t, body, 1); len(evs) != 1 || evs[0].name != EventSnapshot {
		t.Fatalf("first event %v", evs)
	}
	// A ping comment is sent while the job runs.
	if line, _ := body.ReadString('\n'); line != ": ping\n" {
		t.Fatalf("expected ping, got %q", line)
	}

	done := job
	done.Status = api.JobSucceeded
	broker.Publish(job.ID, StreamEvent{Name: EventProgress,
		Data: api.AnalysisProgressEvent{Step: api.StepAnalyzeFit, State: api.StepDone, Progress: 50}})
	broker.Publish(job.ID, StreamEvent{Name: EventCompleted, Data: done})

	evs := readEvents(t, body, 0) // returns at EOF: the server closes after the terminal event
	if got := names(evs); len(got) != 2 || got[0] != EventProgress || got[1] != EventCompleted {
		t.Fatalf("events %v", got)
	}
	var p any
	if err := json.Unmarshal([]byte(evs[0].data), &p); err != nil {
		t.Fatal(err)
	}
	apitest.CheckSchema(t, "AnalysisProgressEvent", p)
}

func TestEventsEndOnClientDisconnect(t *testing.T) {
	job := queuedJob()
	broker := NewBroker()
	h := NewHandler(&fakeJobs{job: job}, fakeLeads{sampleLead()}, fakeSettings{configured()}, &fakeRunner{}, broker)

	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		req := httptest.NewRequest("GET", "/api/analyses/"+job.ID.String()+"/events", nil).WithContext(ctx)
		newRouter(h).ServeHTTP(httptest.NewRecorder(), req)
	}()
	waitFor(t, func() bool { broker.mu.Lock(); defer broker.mu.Unlock(); return len(broker.subs) == 1 })
	cancel()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("handler did not return after the client left")
	}
	if len(broker.subs) != 0 {
		t.Fatal("subscriber leaked after disconnect")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within 1s")
		}
		time.Sleep(time.Millisecond)
	}
}

// ---- Worker → broker --------------------------------------------------------------------

type recordingPublisher struct {
	mu     sync.Mutex
	events []StreamEvent
}

func (p *recordingPublisher) Publish(_ uuid.UUID, ev StreamEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, ev)
}

func TestWorkerPublishesPersistedEvents(t *testing.T) {
	tests := []struct {
		name   string
		script func(context.Context, func(Event) error) error
		want   []string
	}{
		{"completed", func(_ context.Context, h func(Event) error) error {
			_ = h(progress(api.StepValidateInput, api.StepDone, 13))
			return h(Event{Completed: sampleResult()})
		}, []string{EventProgress, EventCompleted}},
		{"failed by ai", func(_ context.Context, h func(Event) error) error {
			return h(Event{Failed: &Failure{Code: "insufficient_data", Message: "More data."}})
		}, []string{EventFailed}},
		{"failed in go", func(context.Context, func(Event) error) error {
			return errors.Join(ErrAIProtocol, errors.New("stream ended"))
		}, []string{EventFailed}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMemStore(queuedJob())
			pub := &recordingPublisher{}
			w := NewWorker(store, fakeSettings{configured()}, &scriptAnalyzer{scripts: []func(context.Context, func(Event) error) error{tt.script}},
				pub, WorkerConfig{Timeout: 5 * time.Second})
			if !w.processNext(context.Background()) {
				t.Fatal("no job claimed")
			}
			got := make([]string, len(pub.events))
			for i, e := range pub.events {
				got[i] = e.Name
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("published %v, want %v", got, tt.want)
			}
			last := pub.events[len(pub.events)-1].Data.(Job)
			if terminalEvent(last.Status) != tt.want[len(tt.want)-1] {
				t.Fatalf("terminal event carries status %s", last.Status)
			}
			apitest.CheckSchema(t, "AnalysisJob", last)
		})
	}
}
