package asyncjob

import (
	"context"
	"errors"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
)

type fakePublishingChannel struct {
	published  amqp.Publishing
	publishErr error
}

type fakeConfirmation struct {
	ack bool
}

func (c fakeConfirmation) Wait() bool {
	return c.ack
}

func (c *fakePublishingChannel) ExchangeDeclare(string, string, bool, bool, bool, bool, amqp.Table) error {
	return nil
}

func (c *fakePublishingChannel) QueueDeclare(string, bool, bool, bool, bool, amqp.Table) (amqp.Queue, error) {
	return amqp.Queue{}, nil
}

func (c *fakePublishingChannel) QueueBind(string, string, string, bool, amqp.Table) error {
	return nil
}

func (c *fakePublishingChannel) PublishConfirmed(_ context.Context, _ string, _ string, _ bool, _ bool, publishing amqp.Publishing) error {
	c.published = publishing
	return c.publishErr
}

func (c *fakePublishingChannel) Close() error {
	return nil
}

func TestRabbitPublisherPublishesPersistentMessage(t *testing.T) {
	channel := &fakePublishingChannel{}
	publisher, err := newRabbitPublisher(channel, "exchange", "jobs")
	if err != nil {
		t.Fatalf("new publisher: %v", err)
	}

	if err := publisher.Publish(context.Background(), Job{ID: "job-1", Type: TypeArticlePublished}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if channel.published.DeliveryMode != amqp.Persistent {
		t.Fatalf("expected persistent delivery mode, got %d", channel.published.DeliveryMode)
	}
}

func TestRabbitPublisherReturnsBrokerConfirmationError(t *testing.T) {
	confirmErr := errors.New("broker did not confirm publication")
	channel := &fakePublishingChannel{publishErr: confirmErr}
	publisher, err := newRabbitPublisher(channel, "exchange", "jobs")
	if err != nil {
		t.Fatalf("new publisher: %v", err)
	}

	err = publisher.Publish(context.Background(), Job{ID: "job-1", Type: TypeArticlePublished})
	if !errors.Is(err, confirmErr) {
		t.Fatalf("expected confirmation error, got %v", err)
	}
}

func TestAwaitConfirmationRejectsBrokerNack(t *testing.T) {
	err := awaitConfirmation(fakeConfirmation{ack: false})
	if err == nil {
		t.Fatal("expected broker nack to fail publication")
	}
}

func TestConfirmingChannelRejectsCanceledContextBeforePublish(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := (&ConfirmingChannel{}).PublishConfirmed(ctx, "exchange", "jobs", false, false, amqp.Publishing{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled context before publication, got %v", err)
	}
}
