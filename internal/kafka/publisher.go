package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/ErenKarakus1/Trading-Engine/internal/matching"
	kafkago "github.com/segmentio/kafka-go"
)

var ErrNilWriter = errors.New("nil writer")

type Writer interface {
	WriteMessages(context.Context, ...kafkago.Message) error
	Close() error
}

type Publisher struct {
	writer Writer
}

func NewWriter(brokers []string, topic string) Writer {
	return &kafkago.Writer{
		Addr:     kafkago.TCP(brokers...),
		Topic:    topic,
		Balancer: &kafkago.Hash{},
	}
}

func NewPublisher(writer Writer) (*Publisher, error) {
	if writer == nil {
		return nil, ErrNilWriter
	}
	return &Publisher{writer: writer}, nil
}

func (p *Publisher) PublishEvents(ctx context.Context, events []matching.Event) error {
	messages := make([]kafkago.Message, 0, len(events))
	for i, event := range events {
		value, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("marshal event %d: %w", i, err)
		}

		messages = append(messages, kafkago.Message{
			Key:   []byte(strconv.FormatInt(int64(event.Sequence), 10)),
			Value: value,
			Headers: []kafkago.Header{
				{Key: "event_type", Value: []byte(event.Type)},
			},
		})
	}

	if len(messages) == 0 {
		return nil
	}
	return p.writer.WriteMessages(ctx, messages...)
}

func (p *Publisher) Close() error {
	return p.writer.Close()
}
