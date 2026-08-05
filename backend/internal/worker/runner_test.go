package worker

import (
	"context"
	"encoding/json"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
	"resource_community_go/internal/asyncjob"
)

type publishedMessage struct {
	exchange   string
	routingKey string
	publishing amqp.Publishing
}

type fakeWorkerChannel struct {
	published []publishedMessage
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

func (c *fakeWorkerChannel) PublishWithContext(_ context.Context, exchange, routingKey string, _ bool, _ bool, publishing amqp.Publishing) error {
	c.published = append(c.published, publishedMessage{
		exchange:   exchange,
		routingKey: routingKey,
		publishing: publishing,
	})
	return nil
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
	beginResult bool
}

func (s *fakeIdempotencyStore) Begin(_ context.Context, jobID string) (bool, error) {
	s.begun = append(s.begun, jobID)
	if !s.beginResult {
		return true, nil
	}
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
		idemStore:   &fakeIdempotencyStore{beginResult: true},
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
