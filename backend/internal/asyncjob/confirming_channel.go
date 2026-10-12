package asyncjob

import (
	"context"
	"errors"

	amqp "github.com/rabbitmq/amqp091-go"
)

type confirmation interface {
	Wait() bool
}

type ConfirmingChannel struct {
	*amqp.Channel
}

func NewConfirmingChannel(conn *amqp.Connection) (*ConfirmingChannel, error) {
	channel, err := conn.Channel()
	if err != nil {
		return nil, err
	}
	if err := channel.Confirm(false); err != nil {
		_ = channel.Close()
		return nil, err
	}
	return &ConfirmingChannel{Channel: channel}, nil
}

func (c *ConfirmingChannel) PublishConfirmed(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	confirmation, err := c.PublishWithDeferredConfirmWithContext(ctx, exchange, key, mandatory, immediate, msg)
	if err != nil {
		return err
	}
	if confirmation == nil {
		return errors.New("rabbitmq publisher confirms are not enabled")
	}
	return awaitConfirmation(confirmation)
}

func awaitConfirmation(confirmation confirmation) error {
	// Once sent, wait for a definitive result so cancellation cannot trigger duplicate fallback work.
	if !confirmation.Wait() {
		return errors.New("rabbitmq broker nacked publication")
	}
	return nil
}
