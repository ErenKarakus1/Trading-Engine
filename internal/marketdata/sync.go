package marketdata

import (
	"context"
	"errors"
)

var ErrUnknownMessage = errors.New("unknown market data message")

type MessageType string

const (
	MessageTypeSnapshot  MessageType = "snapshot"
	MessageTypeUpdate    MessageType = "update"
	MessageTypeHeartbeat MessageType = "heartbeat"
)

type Message struct {
	Type     MessageType
	Snapshot *Snapshot
	Update   *Update
}

type Feed interface {
	Next(context.Context) (Message, error)
	Reconnect(context.Context) error
}

type Syncer struct {
	book       *Book
	feed       Feed
	reconnects int
	heartbeats int
}

func NewSyncer(book *Book, feed Feed) *Syncer {
	return &Syncer{book: book, feed: feed}
}

func (s *Syncer) Run(ctx context.Context) error {
	for {
		message, err := s.feed.Next(ctx)
		if err != nil {
			return err
		}

		if err := s.apply(ctx, message); err != nil {
			return err
		}
	}
}

func (s *Syncer) Apply(ctx context.Context, message Message) error {
	return s.apply(ctx, message)
}

func (s *Syncer) Reconnects() int {
	return s.reconnects
}

func (s *Syncer) Heartbeats() int {
	return s.heartbeats
}

func (s *Syncer) apply(ctx context.Context, message Message) error {
	switch message.Type {
	case MessageTypeSnapshot:
		if message.Snapshot == nil {
			return ErrInvalidSnapshot
		}
		return s.book.ApplySnapshot(*message.Snapshot)
	case MessageTypeUpdate:
		if message.Update == nil {
			return ErrInvalidUpdate
		}
		err := s.book.ApplyUpdate(*message.Update)
		if errors.Is(err, ErrGapDetected) {
			s.reconnects++
			if reconnectErr := s.feed.Reconnect(ctx); reconnectErr != nil {
				return reconnectErr
			}
		}
		return err
	case MessageTypeHeartbeat:
		s.heartbeats++
		return nil
	default:
		return ErrUnknownMessage
	}
}
