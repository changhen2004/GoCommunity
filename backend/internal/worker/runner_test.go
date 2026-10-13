package worker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
	"resource_community_go/internal/asyncjob"
)

type publishedMessage struct {
	exchange   string
	routingKey string
	publishing amqp.Publishing
}

type fakeWorkerChannel struct {
	published  []publishedMessage
	publishErr error
}

func (c *fakeWorkerChannel) ExchangeDeclare(string, string, bool, bool, bool, bool, amqp.Table) error {
	return nil
}

func (c *fakeWorkerChannel) QueueDeclare(string, bool, bool, bool, bool, amqp.Table) (amqp.Queue, error) {
	return amqp.Queue{}, nil
}

func (c *fakeWorkerChannel) QueueBind(string, string, string, bool, amqp.Table) error {
	return nil
}

func (c *fakeWorkerChannel) Consume(string, string, bool, bool, bool, bool, amqp.Table) (<-chan amqp.Delivery, error) {
	return make(chan amqp.Delivery), nil
}

func (c *fakeWorkerChannel) PublishConfirmed(_ context.Context, exchange, routingKey string, _ bool, _ bool, publishing amqp.Publishing) error {
	c.published = append(c.published, publishedMessage{
		exchange:   exchange,
		routingKey: routingKey,
		publishing: publishing,
	})
	return c.publishErr
}

func (c *fakeWorkerChannel) Close() error {
	return nil
}

type fakeAcknowledger struct {
	acked      bool
	nacked     bool
	requeued   bool
	deliveryID uint64
}

func (a *fakeAcknowledger) Ack(tag uint64, multiple bool) error {
	a.acked = true
	a.deliveryID = tag
	return nil
}

func (a *fakeAcknowledger) Nack(tag uint64, multiple bool, requeue bool) error {
	a.nacked = true
	a.requeued = requeue
	a.deliveryID = tag
	return nil
}

func (a *fakeAcknowledger) Reject(tag uint64, requeue bool) error {
	a.nacked = true
	a.requeued = requeue
	a.deliveryID = tag
	return nil
}

type testDelivery struct {
	amqp.Delivery
	acknowledger *fakeAcknowledger
}

type fakeIdempotencyStore struct {
	begun       []string
	completed   []string
	released    []string
	beginResult BeginResult
}

func (s *fakeIdempotencyStore) Begin(_ context.Context, jobID string) (BeginResult, error) {
	s.begun = append(s.begun, jobID)
	return s.beginResult, nil
}

func (s *fakeIdempotencyStore) Complete(_ context.Context, jobID string) error {
	s.completed = append(s.completed, jobID)
	return nil
}

func (s *fakeIdempotencyStore) Release(_ context.Context, jobID string) error {
	s.released = append(s.released, jobID)
	return nil
}

func newTestRunner(channel *fakeWorkerChannel) *Runner {
	return &Runner{
		channel:     channel,
		exchange:    "exchange",
		queue:       "jobs",
		failedQueue: "jobs.dlq",
		maxRetries:  3,
		processor:   NewProcessor(nil, nil),
		idemStore:   &fakeIdempotencyStore{beginResult: BeginAcquired},
	}
}

func newTestDelivery(t *testing.T, job asyncjob.Job, headers amqp.Table) testDelivery {
	t.Helper()

	body, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal job: %v", err)
	}
	return newRawTestDelivery(body, headers)
}

func newRawTestDelivery(body []byte, headers amqp.Table) testDelivery {
	ack := &fakeAcknowledger{}
	return testDelivery{
		Delivery: amqp.Delivery{
			Acknowledger: ack,
			DeliveryTag:  7,
			Body:         body,
			Headers:      headers,
		},
		acknowledger: ack,
	}
}

func TestRunnerRetriesFailedJobBeforeDeadLetter(t *testing.T) {
	channel := &fakeWorkerChannel{}
	runner := newTestRunner(channel)
	idemStore := runner.idemStore.(*fakeIdempotencyStore)
	msg := newTestDelivery(t, asyncjob.Job{ID: "job-1", Type: "unsupported"}, nil)

	runner.handleDelivery(context.Background(), msg.Delivery)

	if len(channel.published) != 1 {
		t.Fatalf("expected retry publish, got %d", len(channel.published))
	}
	if channel.published[0].routingKey != "jobs" {
		t.Fatalf("expected retry to main queue, got %s", channel.published[0].routingKey)
	}
	if channel.published[0].publishing.Headers[RetryCountHeader] != int32(1) {
		t.Fatalf("expected retry count 1, got %#v", channel.published[0].publishing.Headers[RetryCountHeader])
	}
	if !msg.acknowledger.acked {
		t.Fatal("expected original message acked after retry republish")
	}
	if msg.acknowledger.nacked {
		t.Fatal("expected original message not nacked after retry republish")
	}
	if len(idemStore.released) != 1 || idemStore.released[0] != "job-1" {
		t.Fatalf("expected processing idempotency lock released before retry, got %#v", idemStore.released)
	}
}

func TestRunnerRequeuesOriginalWhenRetryIsNotConfirmed(t *testing.T) {
	channel := &fakeWorkerChannel{publishErr: errors.New("broker nacked publication")}
	runner := newTestRunner(channel)
	msg := newTestDelivery(t, asyncjob.Job{ID: "job-1", Type: "unsupported"}, nil)

	runner.handleDelivery(context.Background(), msg.Delivery)

	if msg.acknowledger.acked {
		t.Fatal("expected original message not acked before retry confirmation")
	}
	if !msg.acknowledger.nacked || !msg.acknowledger.requeued {
		t.Fatal("expected original message requeued when retry is not confirmed")
	}
}

func TestRunnerRequeuesOriginalWhenDeadLetterIsNotConfirmed(t *testing.T) {
	channel := &fakeWorkerChannel{publishErr: errors.New("broker nacked publication")}
	runner := newTestRunner(channel)
	msg := newTestDelivery(t, asyncjob.Job{ID: "job-2", Type: "unsupported"}, amqp.Table{
		RetryCountHeader: int32(3),
	})

	runner.handleDelivery(context.Background(), msg.Delivery)

	if msg.acknowledger.acked {
		t.Fatal("expected original message not acked before dead letter confirmation")
	}
	if !msg.acknowledger.nacked || !msg.acknowledger.requeued {
		t.Fatal("expected original message requeued when dead letter is not confirmed")
	}
}

func TestRunnerDeadLettersJobAfterRetryLimit(t *testing.T) {
	channel := &fakeWorkerChannel{}
	runner := newTestRunner(channel)
	idemStore := runner.idemStore.(*fakeIdempotencyStore)
	msg := newTestDelivery(t, asyncjob.Job{ID: "job-2", Type: "unsupported"}, amqp.Table{
		RetryCountHeader: int32(3),
	})

	runner.handleDelivery(context.Background(), msg.Delivery)

	if len(channel.published) != 1 {
		t.Fatalf("expected dlq publish, got %d", len(channel.published))
	}
	if channel.published[0].routingKey != "jobs.dlq" {
		t.Fatalf("expected dlq routing key, got %s", channel.published[0].routingKey)
	}
	if channel.published[0].publishing.Headers[FailureReasonHeader] == "" {
		t.Fatal("expected failure reason header")
	}
	if !msg.acknowledger.acked {
		t.Fatal("expected original message acked after dlq publish")
	}
	if len(idemStore.released) != 1 || idemStore.released[0] != "job-2" {
		t.Fatalf("expected processing idempotency lock released before dlq, got %#v", idemStore.released)
	}
}

func TestRunnerDeadLettersInvalidPayload(t *testing.T) {
	channel := &fakeWorkerChannel{}
	runner := newTestRunner(channel)
	msg := newRawTestDelivery([]byte("{invalid-json"), nil)

	runner.handleDelivery(context.Background(), msg.Delivery)

	if len(channel.published) != 1 {
		t.Fatalf("expected dlq publish, got %d", len(channel.published))
	}
	if channel.published[0].routingKey != "jobs.dlq" {
		t.Fatalf("expected invalid payload in dlq, got %s", channel.published[0].routingKey)
	}
	if !msg.acknowledger.acked {
		t.Fatal("expected invalid payload acked after dlq publish")
	}
	if msg.acknowledger.nacked {
		t.Fatal("expected invalid payload not nacked after dlq publish")
	}
}

func TestRunnerHandlesBeginStates(t *testing.T) {
	for _, tc := range []struct {
		name     string
		state    BeginResult
		jobType  asyncjob.Type
		wantAck  bool
		wantNack bool
		wantDone int
	}{
		{name: "busy", state: BeginBusy, jobType: "unsupported", wantNack: true},
		{name: "done", state: BeginDone, jobType: "unsupported", wantAck: true},
		{name: "acquired", state: BeginAcquired, jobType: asyncjob.TypeArticleViewed, wantAck: true, wantDone: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			channel := &fakeWorkerChannel{}
			runner := newTestRunner(channel)
			store := &fakeIdempotencyStore{beginResult: tc.state}
			runner.idemStore = store
			msg := newTestDelivery(t, asyncjob.Job{ID: "job-state", Type: tc.jobType}, nil)

			runner.handleDelivery(context.Background(), msg.Delivery)

			if msg.acknowledger.acked != tc.wantAck || msg.acknowledger.nacked != tc.wantNack {
				t.Fatalf("unexpected acknowledgement: ack=%v nack=%v", msg.acknowledger.acked, msg.acknowledger.nacked)
			}
			if tc.wantNack && !msg.acknowledger.requeued {
				t.Fatal("expected busy job requeued")
			}
			if len(store.completed) != tc.wantDone || len(store.released) != 0 || len(channel.published) != 0 {
				t.Fatalf("unexpected side effects: completed=%v released=%v published=%d", store.completed, store.released, len(channel.published))
			}
		})
	}
}

type failingCompleteStore struct {
	IdempotencyStore
}

func (failingCompleteStore) Complete(context.Context, string) error {
	return errors.New("temporary Redis completion failure")
}

func TestRunnerRequeuesProcessingRedeliveryAfterCompletionFailure(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()
	store := NewRedisIdempotencyStore(client)
	channel := &fakeWorkerChannel{}
	runner := newTestRunner(channel)
	runner.idemStore = failingCompleteStore{IdempotencyStore: store}
	job := asyncjob.Job{ID: "job-completion-failure", Type: asyncjob.TypeArticleViewed}

	for attempt := 0; attempt < 2; attempt++ {
		msg := newTestDelivery(t, job, nil)
		runner.handleDelivery(context.Background(), msg.Delivery)
		if msg.acknowledger.acked || !msg.acknowledger.nacked || !msg.acknowledger.requeued {
			t.Fatalf("attempt %d: expected requeue without ack, got %+v", attempt, msg.acknowledger)
		}
	}
	if value, err := mr.Get(store.prefix + job.ID); err != nil || value != "processing" {
		t.Fatalf("expected processing state retained, got %q, err=%v", value, err)
	}
	if len(channel.published) != 0 {
		t.Fatal("busy redelivery must not increment business retries or publish to the dead-letter queue")
	}
}
