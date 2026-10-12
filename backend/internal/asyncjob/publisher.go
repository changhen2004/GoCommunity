package asyncjob

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

type Publisher interface {
	Publish(ctx context.Context, job Job) error
}

type NoopPublisher struct{}

func (NoopPublisher) Publish(context.Context, Job) error {
	return nil
}

type RabbitPublisher struct {
	channel  publishingChannel
	exchange string
	queue    string
}

type publishingChannel interface {
	ExchangeDeclare(name, kind string, durable, autoDelete, internal, noWait bool, args amqp.Table) error
	QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error)
	QueueBind(name, key, exchange string, noWait bool, args amqp.Table) error
	PublishConfirmed(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error
	Close() error
}

func NewRabbitPublisher(conn *amqp.Connection, exchange, queue string) (*RabbitPublisher, error) {
	channel, err := NewConfirmingChannel(conn)
	if err != nil {
		return nil, err
	}
	return newRabbitPublisher(channel, exchange, queue)
}

func newRabbitPublisher(channel publishingChannel, exchange, queue string) (*RabbitPublisher, error) {
	if err := channel.ExchangeDeclare(exchange, "direct", true, false, false, false, nil); err != nil {
		_ = channel.Close()
		return nil, err
	}
	if _, err := channel.QueueDeclare(queue, true, false, false, false, nil); err != nil {
		_ = channel.Close()
		return nil, err
	}
	if err := channel.QueueBind(queue, queue, exchange, false, nil); err != nil {
		_ = channel.Close()
		return nil, err
	}

	return &RabbitPublisher{
		channel:  channel,
		exchange: exchange,
		queue:    queue,
	}, nil
}

func (p *RabbitPublisher) Publish(ctx context.Context, job Job) error {
	if job.ID == "" {
		job.ID = uuid.NewString()
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now().UTC()
	}

	body, err := json.Marshal(job)
	if err != nil {
		return err
	}

	return p.channel.PublishConfirmed(ctx, p.exchange, p.queue, false, false, amqp.Publishing{
		ContentType:  "application/json",
		Body:         body,
		DeliveryMode: amqp.Persistent,
	})
}

func (p *RabbitPublisher) Close() error {
	if p == nil || p.channel == nil {
		return nil
	}
	return p.channel.Close()
}
