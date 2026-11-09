package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

// ACP elicitation (ADR-0055 section 4): an agent asks the client for
// structured input with `elicitation/create`. This client advertises only the
// `form` mode and only when an ElicitationPolicy is installed (WithElicitation):
// an agent never sends a request for a mode or a capability the client did not
// declare. A `url` request (a link handed to the human) is refused whatever the
// policy says: smind has no flow to complete one.

// Elicitation actions of an ElicitationResponse.
const (
	ElicitationAccept  = "accept"
	ElicitationDecline = "decline"
	ElicitationCancel  = "cancel"
)

// ElicitationOption is one titled choice of an enum property (a `oneOf` entry,
// or a plain `enum` value with an empty Title).
type ElicitationOption struct {
	Value string
	Title string
}

// ElicitationProperty is one field of a requested form, reduced to what a
// policy needs. Type is the JSON schema type of the property ("string",
// "number", "integer", "boolean", "array"). Options is the closed choice list of
// a single-select string (enum / oneOf) or the item choices of a multi-select
// array. Unsupported is true when the property uses a shape this client does
// not model (a nested object, an unknown type, an array of non-strings): a
// policy must then refuse the whole form.
type ElicitationProperty struct {
	Name        string
	Type        string
	Title       string
	Description string
	Format      string
	Options     []ElicitationOption
	MinLength   *int
	MaxLength   *int
	Pattern     string
	Minimum     *float64
	Maximum     *float64
	Unsupported bool
}

// ElicitationRequest is a decoded `elicitation/create` form request. Properties
// are sorted by name (the protocol sends an unordered object).
type ElicitationRequest struct {
	SessionID  string
	Message    string
	Mode       string
	Properties []ElicitationProperty
	Required   []string
}

// ElicitationResponse is a policy's answer. Content (accept only) maps a
// property name to a string, a number, a boolean or a []string.
type ElicitationResponse struct {
	Action  string
	Content map[string]any
}

// ElicitationPolicy decides an elicitation request, blocking until a human (or
// a policy) has answered. Like PermissionPolicy, a slow Decide is expected.
type ElicitationPolicy interface {
	Decide(ctx context.Context, req ElicitationRequest) (ElicitationResponse, error)
}

// WithElicitation installs p and makes the client advertise the elicitation
// `form` capability at initialize. Without it the capability is absent and an
// `elicitation/create` request is answered "method not found".
func WithElicitation(p ElicitationPolicy) Option {
	return func(c *Client) { c.elicitation = p }
}

type elicitationCapabilities struct {
	Form struct{} `json:"form"`
}

type rawElicitationRequest struct {
	Mode            string          `json:"mode"`
	SessionID       string          `json:"sessionId"`
	Message         string          `json:"message"`
	RequestedSchema json.RawMessage `json:"requestedSchema"`
}

type rawElicitationSchema struct {
	Properties map[string]json.RawMessage `json:"properties"`
	Required   []string                   `json:"required"`
}

type rawElicitationProperty struct {
	Type        string          `json:"type"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Format      string          `json:"format"`
	Enum        []string        `json:"enum"`
	OneOf       []rawConstTitle `json:"oneOf"`
	MinLength   *int            `json:"minLength"`
	MaxLength   *int            `json:"maxLength"`
	Pattern     string          `json:"pattern"`
	Minimum     *float64        `json:"minimum"`
	Maximum     *float64        `json:"maximum"`
	Items       *rawElicitItems `json:"items"`
}

type rawConstTitle struct {
	Const string `json:"const"`
	Title string `json:"title"`
}

type rawElicitItems struct {
	Type  string          `json:"type"`
	Enum  []string        `json:"enum"`
	AnyOf []rawConstTitle `json:"anyOf"`
}

// decodeElicitation parses a form request. A schema that is not an object, or
// a property that cannot be decoded, is reported through Unsupported (the
// policy refuses) rather than an error, so one odd field never turns into a
// silent protocol failure.
func decodeElicitation(raw json.RawMessage) (ElicitationRequest, error) {
	var in rawElicitationRequest
	if err := json.Unmarshal(raw, &in); err != nil {
		return ElicitationRequest{}, err
	}
	req := ElicitationRequest{SessionID: in.SessionID, Message: in.Message, Mode: in.Mode}
	if in.Mode == "" {
		req.Mode = "form"
	}
	var schema rawElicitationSchema
	if len(in.RequestedSchema) > 0 {
		if err := json.Unmarshal(in.RequestedSchema, &schema); err != nil {
			return req, fmt.Errorf("requestedSchema: %w", err)
		}
	}
	req.Required = schema.Required
	names := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		var p rawElicitationProperty
		prop := ElicitationProperty{Name: name}
		if err := json.Unmarshal(schema.Properties[name], &p); err != nil {
			prop.Unsupported = true
			req.Properties = append(req.Properties, prop)
			continue
		}
		prop.Type, prop.Title, prop.Description, prop.Format = p.Type, p.Title, p.Description, p.Format
		prop.MinLength, prop.MaxLength, prop.Pattern, prop.Minimum, prop.Maximum = p.MinLength, p.MaxLength, p.Pattern, p.Minimum, p.Maximum
		switch p.Type {
		case "string":
			if len(p.OneOf) > 0 {
				for _, o := range p.OneOf {
					prop.Options = append(prop.Options, ElicitationOption{Value: o.Const, Title: o.Title})
				}
			} else {
				for _, v := range p.Enum {
					prop.Options = append(prop.Options, ElicitationOption{Value: v})
				}
			}
		case "number", "integer", "boolean":
		case "array":
			switch {
			case p.Items == nil || p.Items.Type != "string":
				prop.Unsupported = true
			case len(p.Items.AnyOf) > 0:
				for _, o := range p.Items.AnyOf {
					prop.Options = append(prop.Options, ElicitationOption{Value: o.Const, Title: o.Title})
				}
			default:
				for _, v := range p.Items.Enum {
					prop.Options = append(prop.Options, ElicitationOption{Value: v})
				}
			}
			if len(prop.Options) == 0 {
				prop.Unsupported = true
			}
		default:
			prop.Unsupported = true
		}
		req.Properties = append(req.Properties, prop)
	}
	return req, nil
}

type elicitationResult struct {
	Action  string         `json:"action"`
	Content map[string]any `json:"content,omitempty"`
}

func (c *Client) handleElicitationCreate(ctx context.Context, raw json.RawMessage) (any, *RPCError) {
	p := c.elicitation
	if p == nil {
		return nil, &RPCError{Code: ErrCodeMethodNotFound, Message: "method not found: elicitation/create"}
	}
	req, err := decodeElicitation(raw)
	if err != nil {
		return nil, &RPCError{Code: ErrCodeInvalidParams, Message: "elicitation/create: " + err.Error()}
	}
	// Only the form mode was advertised; a url (or any other) mode is refused.
	if req.Mode != "form" {
		return elicitationResult{Action: ElicitationDecline}, nil
	}
	resp, err := p.Decide(ctx, req)
	if err != nil {
		return nil, &RPCError{Code: ErrCodeInternalError, Message: "elicitation/create: " + err.Error()}
	}
	switch resp.Action {
	case ElicitationAccept, ElicitationDecline, ElicitationCancel:
	default:
		return nil, &RPCError{Code: ErrCodeInternalError, Message: "elicitation/create: policy returned action " + resp.Action}
	}
	out := elicitationResult{Action: resp.Action}
	if resp.Action == ElicitationAccept {
		out.Content = resp.Content
	}
	return out, nil
}

// handleElicitationComplete ignores `elicitation/complete` (a url-mode
// completion notice; this client never advertises url mode).
func (c *Client) handleElicitationComplete(json.RawMessage) {}
