package push

import "context"

const (
	ProviderExpo    = "expo"
	PlatformAndroid = "android"
	PlatformIOS     = "ios"

	// ChannelAttention is the Android notification channel the app
	// registers for attention-required events (ADR-0040 §1).
	ChannelAttention = "attention"

	TTLSeconds = 86400

	// GenericTitle and GenericBody are the fixed, content-free alert iOS
	// shows when the notification service extension cannot rewrite it with
	// the decrypted text.
	GenericTitle = "smind"
	GenericBody  = "Open smind to see what happened"
)

// Message is one push to one device: Token + Platform say where, Data is the
// sealed blob.
type Message struct {
	Token    string
	Platform string
	Data     Blob
}

// Result is the outcome for one Message (same index). OK true means the
// provider accepted it. ErrCode is the provider's error code (e.g.
// "DeviceNotRegistered") or "".
type Result struct {
	Token   string
	OK      bool
	ErrCode string
}

// Sender delivers sealed messages through a push provider, selected by the
// registration's provider field (ADR-0040 §2).
type Sender interface {
	Send(ctx context.Context, msgs []Message) ([]Result, error)
}

// expoMessage is one entry of the Expo Push API's JSON array body. The
// sealed Blob rides as the message "data"; nothing here can name a task.
type expoMessage struct {
	To             string `json:"to"`
	Data           Blob   `json:"data"`
	Priority       string `json:"priority"`
	TTL            int    `json:"ttl"`
	ChannelID      string `json:"channelId,omitempty"`
	Title          string `json:"title,omitempty"`
	Body           string `json:"body,omitempty"`
	MutableContent bool   `json:"mutableContent,omitempty"`
}

// buildExpo converts m to the Expo wire object. Android gets a data-only
// message (a high-priority data message that shows nothing risks being
// deprioritized by FCM, so the app's background task shows the
// notification); iOS gets a fixed generic alert with mutableContent so the
// notification service extension can rewrite it after decrypting.
func buildExpo(m Message) expoMessage {
	msg := expoMessage{
		To:       m.Token,
		Data:     m.Data,
		Priority: "high",
		TTL:      TTLSeconds,
	}
	if m.Platform == PlatformIOS {
		msg.Title = GenericTitle
		msg.Body = GenericBody
		msg.MutableContent = true
	} else {
		msg.ChannelID = ChannelAttention
	}
	return msg
}
