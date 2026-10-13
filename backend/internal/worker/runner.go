package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"resource_community_go/internal/asyncjob"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	DefaultMaxRetryCount = 3
	RetryCountHeader     = "x-retry-count"
	FailureReasonHeader  = "x-failure-reason"
)

type amqpChannel interface {
	ExchangeDeclare(name, kind string, durable, autoDelete, internal, noWait bool, args amqp.Table) error
	QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error)
	QueueBind(name, key, exchange string, noWait bool, args amqp.Table) error
	Consume(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error)
	PublishConfirmed(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error
	Close() error
}

type Runner struct {
	channel     amqpChannel
	idemStore   IdempotencyStore
	exchange    string
	queue       string
	failedQueue string
	maxRetries  int
	processor   *Processor
}

func NewRunner(conn *amqp.Connection, exchange, queue string, processor *Processor, idemStore IdempotencyStore) (*Runner, error) {
	ch, err := asyncjob.NewConfirmingChannel(conn)
	if err != nil {
		return nil, err
	}

	if err := ch.ExchangeDeclare(exchange, "direct", true, false, false, false, nil); err != nil {
		_ = ch.Close()
		return nil, err
	}
	if _, err := ch.QueueDeclare(queue, true, false, false, false, nil); err != nil {
		_ = ch.Close()
		return nil, err
	}
	if err := ch.QueueBind(queue, queue, exchange, false, nil); err != nil {
		_ = ch.Close()
		return nil, err
	}
	failedQueue := queue + ".dlq"
	if _, err := ch.QueueDeclare(failedQueue, true, false, false, false, nil); err != nil {
		_ = ch.Close()
		return nil, err
	}
	if err := ch.QueueBind(failedQueue, failedQueue, exchange, false, nil); err != nil {
		_ = ch.Close()
		return nil, err
	}

	if idemStore == nil {
		idemStore = NoopIdempotencyStore{}
	}

	return &Runner{
		channel:     ch,
		idemStore:   idemStore,
		exchange:    exchange,
		queue:       queue,
		failedQueue: failedQueue,
		maxRetries:  DefaultMaxRetryCount,
		processor:   processor,
	}, nil
}

func (w *Runner) Run(ctx context.Context) error {
	msgs, err := w.channel.Consume(w.queue, "", false, false, false, false, nil)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-msgs:
			if !ok {
				return errors.New("worker delivery channel closed")
			}
			w.handleDelivery(ctx, msg)
		}
	}
}

func (w *Runner) handleDelivery(ctx context.Context, msg amqp.Delivery) {
	var job asyncjob.Job
	if err := json.Unmarshal(msg.Body, &job); err != nil {
		log.Printf("invalid async job payload: %v", err)
		if deadLetterErr := w.deadLetter(ctx, msg, fmt.Sprintf("invalid payload: %v", err)); deadLetterErr != nil {
			log.Printf("dead letter invalid payload failed: %v", deadLetterErr)
			_ = msg.Nack(false, true)
			return
		}
		_ = msg.Ack(false)
		return
	}

	result, err := w.idemStore.Begin(ctx, job.ID)
	if err != nil {
		log.Printf("idempotency begin failed: jobID=%s err=%v", job.ID, err)
		_ = msg.Nack(false, true)
		return
	}

	switch result {
	case BeginDone:
		_ = msg.Ack(false)
		return

	case BeginBusy:
		// ponytail: local backoff pauses this Worker; use a delayed queue if contention affects throughput.
		timer := time.NewTimer(time.Second)
		defer timer.Stop()

		select {
		case <-timer.C:
		case <-ctx.Done():
		}
		_ = msg.Nack(false, true)
		return

	case BeginAcquired:

	default:
		log.Printf("unexpected begin result: jobID=%s result=%d", job.ID, result)
		_ = msg.Nack(false, true)
		return

	}
	if err := w.processor.Handle(ctx, job); err != nil {
		log.Printf("process async job failed: type=%s err=%v", job.Type, err)
		w.handleProcessingFailure(ctx, msg, job, err)
		return
	}

	if err := w.idemStore.Complete(ctx, job.ID); err != nil {
		log.Printf("idempotency complete failed: jobID=%s err=%v", job.ID, err)
		_ = msg.Nack(false, true)
		return
	}

	_ = msg.Ack(false)
}

func (w *Runner) handleProcessingFailure(ctx context.Context, msg amqp.Delivery, job asyncjob.Job, cause error) {
	if err := w.idemStore.Release(ctx, job.ID); err != nil {
		log.Printf("idempotency release failed: jobID=%s err=%v", job.ID, err)
		_ = msg.Nack(false, true)
		return
	}

	if w.retryCount(msg.Headers) >= w.effectiveMaxRetries() {
		if err := w.deadLetter(ctx, msg, cause.Error()); err != nil {
			log.Printf("dead letter async job failed: err=%v", err)
			_ = msg.Nack(false, true)
			return
		}
		_ = msg.Ack(false)
		return
	}

	if err := w.retryJob(ctx, msg); err != nil {
		log.Printf("retry async job publish failed: err=%v", err)
		_ = msg.Nack(false, true)
		return
	}
	_ = msg.Ack(false)
}

func (w *Runner) retryJob(ctx context.Context, msg amqp.Delivery) error {
	headers := cloneHeaders(msg.Headers)
	headers[RetryCountHeader] = int32(w.retryCount(msg.Headers) + 1)
	return w.channel.PublishConfirmed(ctx, w.exchange, w.queue, false, false, amqp.Publishing{
		ContentType:  contentTypeOrDefault(msg.ContentType),
		Body:         msg.Body,
		Headers:      headers,
		DeliveryMode: amqp.Persistent,
	})
}

func (w *Runner) deadLetter(ctx context.Context, msg amqp.Delivery, reason string) error {
	headers := cloneHeaders(msg.Headers)
	headers[RetryCountHeader] = int32(w.retryCount(msg.Headers))
	headers[FailureReasonHeader] = reason
	return w.channel.PublishConfirmed(ctx, w.exchange, w.effectiveFailedQueue(), false, false, amqp.Publishing{
		ContentType:  contentTypeOrDefault(msg.ContentType),
		Body:         msg.Body,
		Headers:      headers,
		DeliveryMode: amqp.Persistent,
	})
}

func (w *Runner) retryCount(headers amqp.Table) int {
	if headers == nil {
		return 0
	}

	switch value := headers[RetryCountHeader].(type) {
	case int:
		return value
	case int8:
		return int(value)
	case int16:
		return int(value)
	case int32:
		return int(value)
	case int64:
		return int(value)
	case uint:
		return int(value)
	case uint8:
		return int(value)
	case uint16:
		return int(value)
	case uint32:
		return int(value)
	case uint64:
		return int(value)
	default:
		return 0
	}
}

func (w *Runner) effectiveMaxRetries() int {
	if w.maxRetries < 1 {
		return DefaultMaxRetryCount
	}
	return w.maxRetries
}

func (w *Runner) effectiveFailedQueue() string {
	if w.failedQueue == "" {
		return w.queue + ".dlq"
	}
	return w.failedQueue
}

func cloneHeaders(headers amqp.Table) amqp.Table {
	cloned := amqp.Table{}
	for key, value := range headers {
		cloned[key] = value
	}
	return cloned
}

func contentTypeOrDefault(contentType string) string {
	if contentType == "" {
		return "application/json"
	}
	return contentType
}

func (w *Runner) Close() error {
	if w == nil || w.channel == nil {
		return nil
	}
	return w.channel.Close()
}
