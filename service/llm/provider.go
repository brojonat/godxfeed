// Package llm translates natural-language subscription requests into
// structured filter specs, then into concrete (event, symbol) pairs
// ready to hand to the SubscriptionManager.
//
// The design is deliberately two-step:
//
//  1. A Provider (Anthropic / OpenAI / Gemini) extracts a FilterSpec
//     from user text. This is the LLM's only job — narrow, testable,
//     cheap. No tool loop, no multi-turn, no open-ended picking of
//     symbols from a chain the model doesn't have in context.
//
//  2. A Resolver takes the FilterSpec and expands it against tastytrade
//     REST (equity streamer symbol + option chain) into a deterministic
//     list of subscriptions. This step owns all the "is this expiry
//     within the window" / "is this strike between min and max" logic;
//     it's unit-tested with no LLM in the loop.
//
// The Provider never sees the option chain — just the user's text and
// today's date. That keeps prompts short (cheap + fast) and prevents
// a failure mode where the LLM picks strikes that don't exist.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// FilterSpec is the structured intent the LLM extracts from a user
// request. Every field is optional except Root; defaults are applied
// downstream by the Resolver.
//
// Design goals:
//   - Flat, JSON-serializable (LLMs return this directly).
//   - Minimal vocabulary — the LLM should never need more than the
//     fields here to express any reasonable subscription request.
//   - Null-friendly — unset windows are nil (not sentinel values), so
//     "all expiries" / "all strikes" is expressible without magic.
type FilterSpec struct {
	// Root is the underlying ticker the user named (e.g. "SPY",
	// "AAPL"). Required. Case is normalized to upper by the Resolver.
	Root string `json:"root"`

	// EventTypes is the dxFeed event streams to subscribe. Defaults
	// to []string{"Quote"} when empty. Allowed values: "Quote",
	// "Greeks", "TheoPrice", "Underlying".
	EventTypes []string `json:"event_types"`

	// Kind filters option type:
	//   "call"  → calls only
	//   "put"   → puts only
	//   "both"  → calls and puts
	//   ""      → no options (equity only)
	// The Resolver refuses "call"/"put"/"both" without an event type
	// that makes sense for options (anything other than equity Quote).
	Kind string `json:"kind"`

	// ExpiryWindow bounds option expiries to [Min, Max] inclusive.
	// Dates are "YYYY-MM-DD". Nil means "all expiries".
	ExpiryWindow *DateWindow `json:"expiry_window"`

	// StrikeWindow bounds option strikes to [Min, Max] inclusive.
	// Nil means "all strikes".
	StrikeWindow *Range `json:"strike_window"`

	// IncludeEquity controls whether the underlying equity is
	// subscribed alongside any option matches. True by default when
	// Kind == "" (pure equity request) and honored as-is otherwise.
	IncludeEquity bool `json:"include_equity"`
}

// DateWindow is an inclusive [Min, Max] date range, "YYYY-MM-DD".
type DateWindow struct {
	Min string `json:"min"`
	Max string `json:"max"`
}

// Range is an inclusive [Min, Max] numeric range.
type Range struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

// Provider extracts a FilterSpec from natural-language user text.
// Implementations must be safe for concurrent use.
//
// Each provider handles its own wire format (Anthropic tool_use,
// OpenAI response_format.json_schema, Gemini response_schema) — the
// shared abstraction is "text in, FilterSpec out."
type Provider interface {
	// Name returns the provider's canonical name (e.g. "anthropic",
	// "openai", "gemini"). Used for logging + the HTTP handler's
	// provider-picker.
	Name() string

	// Extract turns userText into a FilterSpec. The `now` value is
	// threaded into the prompt so relative dates ("next Friday", "in
	// March") resolve deterministically. Pass time.Now() in
	// production; inject a fixed clock in tests.
	Extract(ctx context.Context, userText string, now time.Time) (FilterSpec, error)
}

// FilterSchemaJSON is the JSON Schema for FilterSpec, shared across
// all three providers (Anthropic's input_schema, OpenAI's json_schema,
// Gemini's response_schema). Kept as a string constant so the exact
// bytes are identical at every call site — if drift between providers
// ever bit us, we'd want it to be visible at code-review time.
const FilterSchemaJSON = `{
  "type": "object",
  "properties": {
    "root": {
      "type": "string",
      "description": "Underlying ticker the user named (e.g. SPY, AAPL). Required."
    },
    "event_types": {
      "type": "array",
      "items": {
        "type": "string",
        "enum": ["Quote", "Greeks", "TheoPrice", "Underlying"]
      },
      "description": "dxFeed event streams to subscribe. Default [\"Quote\"] if empty."
    },
    "kind": {
      "type": "string",
      "enum": ["call", "put", "both", ""],
      "description": "Option kind filter. Empty string means equity only (no options)."
    },
    "expiry_window": {
      "type": ["object", "null"],
      "properties": {
        "min": {"type": "string", "description": "Inclusive min expiry, YYYY-MM-DD"},
        "max": {"type": "string", "description": "Inclusive max expiry, YYYY-MM-DD"}
      },
      "required": ["min", "max"],
      "additionalProperties": false
    },
    "strike_window": {
      "type": ["object", "null"],
      "properties": {
        "min": {"type": "number"},
        "max": {"type": "number"}
      },
      "required": ["min", "max"],
      "additionalProperties": false
    },
    "include_equity": {
      "type": "boolean",
      "description": "Also subscribe the underlying equity quote."
    }
  },
  "required": ["root", "event_types", "kind", "expiry_window", "strike_window", "include_equity"],
  "additionalProperties": false
}`

// SystemPrompt returns the prompt that every provider sends. "now" is
// embedded verbatim so relative-date phrases in user text resolve to
// concrete YYYY-MM-DD values the Resolver can filter on.
//
// Kept deliberately short — the schema itself documents the field
// semantics, so the prompt just has to anchor the model to the task
// and the calendar.
func SystemPrompt(now time.Time) string {
	return fmt.Sprintf(`You are a subscription-filter extractor for a market-data service.
Given a trader's natural-language request, emit a JSON object matching
the provided schema. Extract only what the user implied; do not invent
symbols, strikes, or expiries.

Today is %s. Resolve all relative dates (e.g. "next Friday", "in March",
"expiring this month") to absolute YYYY-MM-DD values using this date.

If the user asks for options in a kind-unambiguous way ("SPY calls
expiring Friday"), set kind accordingly and include_equity=false unless
they also asked for the underlying. If the user says just "SPY" with no
option qualifiers, set kind="" and include_equity=true.

Default event_types to ["Quote"] if none is specified. Use "Greeks"
only when the user mentions delta/gamma/theta/vega/volatility or
explicitly asks for Greeks; "TheoPrice" when they mention theoretical
price; "Underlying" when they mention IV/putCallRatio/front-back vol.`,
		now.Format("2006-01-02 (Monday)"))
}

// parseFilterJSON decodes the raw JSON the LLM emitted into a
// FilterSpec, and applies the defaults/invariants the Resolver
// depends on (uppercase root, Quote-default event types).
//
// Kept here (not in the Resolver) so every provider passes through
// the same normalization — a caller that reads FilterSpec from the
// HTTP response gets the same thing the Resolver would get.
func parseFilterJSON(raw []byte) (FilterSpec, error) {
	var f FilterSpec
	if err := json.Unmarshal(raw, &f); err != nil {
		return FilterSpec{}, fmt.Errorf("llm: parse filter json: %w", err)
	}
	return f, nil
}
