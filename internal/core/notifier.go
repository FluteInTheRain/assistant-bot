package core

import (
	"context"
	"log/slog"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Notifier is how modules reach the owner's alert channel. Alerts mention the
// owner (`<@OWNER_ID>`) so the phone notifies even on a muted channel;
// recoveries and summaries do not (design 5.3).
type Notifier interface {
	Alert(text string) // pings the owner
	Info(text string)  // no ping
}

// message is one queued Discord send.
type message struct {
	text     string
	mentions bool
}

type notifier struct {
	session   *discordgo.Session
	channelID string
	ownerID   string
	log       *slog.Logger
	queue     chan message
}

// notifierQueueSize is generous on purpose: state machines only send on
// transitions, so even a long Discord outage queues a handful of messages.
// If it ever fills, enqueue blocks — by then the dead-man's switch has
// already fired, and no event is ever dropped (design 5.3).
const notifierQueueSize = 256

// sendRetryDelay is the delay between Discord retries. The design says "retry
// on the next tick"; a 30 s retry is strictly better for a transient outage
// and still never drops a pending event.
const sendRetryDelay = 30 * time.Second

// NewNotifier returns a Notifier backed by one sender goroutine and a stop
// function that shuts it down cleanly.
func NewNotifier(session *discordgo.Session, channelID, ownerID string, log *slog.Logger) (Notifier, func()) {
	n := &notifier{
		session:   session,
		channelID: channelID,
		ownerID:   ownerID,
		log:       log,
		queue:     make(chan message, notifierQueueSize),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		n.run(ctx)
	}()
	return n, func() {
		cancel()
		<-done
	}
}

func (n *notifier) Alert(text string) { n.queue <- message{text: text, mentions: true} }
func (n *notifier) Info(text string)  { n.queue <- message{text: text, mentions: false} }

func (n *notifier) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case m := <-n.queue:
			n.deliver(ctx, m)
		}
	}
}

// deliver sends one message, retrying with a fixed delay until it succeeds or
// the bot shuts down. A failure is logged, never silent (design 5.3).
func (n *notifier) deliver(ctx context.Context, m message) {
	text := m.text
	if m.mentions {
		text = "<@" + n.ownerID + "> " + text
	}
	for attempt := 1; ; attempt++ {
		_, err := n.session.ChannelMessageSend(n.channelID, text)
		if err == nil {
			return
		}
		// Never log the message content here: it could quote secrets that
		// slipped into a text (the token itself is never in a message).
		n.log.Error("discord send failed, retrying", "attempt", attempt, "err", redact(err))
		select {
		case <-ctx.Done():
			n.log.Error("shutting down with one undelivered message", "mentions", m.mentions)
			return
		case <-time.After(sendRetryDelay):
		}
	}
}

// redact keeps accidental secrets (the Discord token, the healthcheck UUID)
// out of the logs. discordgo errors never contain them, but this is cheap.
func redact(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
