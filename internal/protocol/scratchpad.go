package protocol

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/identifier"
	"github.com/akonwi/kit/internal/scratchpad"
)

// ScratchpadRevision is a positive signed 64-bit revision encoded as a
// canonical decimal JSON string so browser clients cannot lose precision.
type ScratchpadRevision int64

func (revision ScratchpadRevision) MarshalJSON() ([]byte, error) {
	if revision < 1 {
		return nil, fmt.Errorf("scratchpad revision must be positive")
	}
	return json.Marshal(strconv.FormatInt(int64(revision), 10))
}

func (revision *ScratchpadRevision) UnmarshalJSON(data []byte) error {
	if revision == nil {
		return fmt.Errorf("scratchpad revision target is nil")
	}
	if len(data) < 3 || data[0] != '"' || data[len(data)-1] != '"' {
		return fmt.Errorf("scratchpad revision must be a decimal string")
	}
	raw := data[1 : len(data)-1]
	if len(raw) == 0 || len(raw) > 19 || len(raw) > 1 && raw[0] == '0' {
		return fmt.Errorf("scratchpad revision is not canonical")
	}
	for _, digit := range raw {
		if digit < '0' || digit > '9' {
			return fmt.Errorf("scratchpad revision is not canonical")
		}
	}
	encoded := string(raw)
	parsed, err := strconv.ParseInt(encoded, 10, 64)
	if err != nil || parsed < 1 || strconv.FormatInt(parsed, 10) != encoded {
		return fmt.Errorf("scratchpad revision is not canonical")
	}
	*revision = ScratchpadRevision(parsed)
	return nil
}

// Scratchpad is the authoritative shared record for one session family.
type Scratchpad struct {
	OwnerSessionID string             `json:"ownerSessionId"`
	Content        string             `json:"content"`
	Revision       ScratchpadRevision `json:"revision"`
	UpdatedAt      string             `json:"updatedAt"`
}

// UpdateScratchpadInput requests one compare-and-swap content replacement.
type UpdateScratchpadInput struct {
	ExpectedRevision ScratchpadRevision `json:"expectedRevision"`
	Content          string             `json:"content"`
}

// Validate checks an authoritative scratchpad crossing a transport boundary.
func (record Scratchpad) Validate() error {
	if !identifier.Valid(record.OwnerSessionID, "session_") {
		return fmt.Errorf("scratchpad owner session id is invalid")
	}
	if err := scratchpad.ValidateContent(record.Content); err != nil {
		return err
	}
	if err := scratchpad.ValidateRevision(int64(record.Revision)); err != nil {
		return err
	}
	parsed, err := time.Parse(time.RFC3339Nano, record.UpdatedAt)
	if err != nil || parsed.Location() != time.UTC || parsed.Format(time.RFC3339Nano) != record.UpdatedAt {
		return fmt.Errorf("scratchpad updated time is not canonical UTC RFC 3339")
	}
	return nil
}

// Validate checks a scratchpad mutation crossing a transport boundary.
func (input UpdateScratchpadInput) Validate() error {
	if err := scratchpad.ValidateRevision(int64(input.ExpectedRevision)); err != nil {
		return err
	}
	return scratchpad.ValidateContent(input.Content)
}

// ValidateApplied checks a mutation response against its initiating request.
func (record Scratchpad) ValidateApplied(input UpdateScratchpadInput) error {
	if err := input.Validate(); err != nil {
		return err
	}
	if err := record.Validate(); err != nil {
		return err
	}
	revisionMatches := record.Revision == input.ExpectedRevision ||
		(input.ExpectedRevision < ScratchpadRevision(math.MaxInt64) && record.Revision == input.ExpectedRevision+1)
	if record.Content != input.Content || !revisionMatches {
		return fmt.Errorf("scratchpad result does not match requested content or revision")
	}
	return nil
}

// ScratchpadErrorCode is one stable scratchpad API failure identity.
type ScratchpadErrorCode string

const (
	ScratchpadInvalidContent    ScratchpadErrorCode = "scratchpad_invalid_content"
	ScratchpadTooLarge          ScratchpadErrorCode = "scratchpad_too_large"
	ScratchpadRevisionConflict  ScratchpadErrorCode = "scratchpad_revision_conflict"
	ScratchpadRevisionExhausted ScratchpadErrorCode = "scratchpad_revision_exhausted"
	ScratchpadMigrationRequired ScratchpadErrorCode = "scratchpad_migration_required"
	ScratchpadUnsupported       ScratchpadErrorCode = "scratchpad_unsupported"
	ScratchpadUnavailable       ScratchpadErrorCode = "scratchpad_unavailable"
)

// ScratchpadError is one stable failure from a scratchpad operation.
type ScratchpadError struct {
	Code    ScratchpadErrorCode
	Message string
	Current *Scratchpad
}

func (err *ScratchpadError) Error() string {
	if err == nil {
		return "scratchpad error"
	}
	return err.Message
}

// Validate checks a typed scratchpad failure crossing a transport boundary.
func (err *ScratchpadError) Validate() error {
	if err == nil {
		return fmt.Errorf("scratchpad error is nil")
	}
	if validationErr := err.Code.Validate(); validationErr != nil {
		return validationErr
	}
	if !validRendererText(err.Message, 1024) || strings.TrimSpace(err.Message) == "" {
		return fmt.Errorf("scratchpad error message is invalid")
	}
	return (ScratchpadErrorDetails{Scratchpad: err.Current}).Validate(err.Code)
}

// ScratchpadErrorDetails carries the authoritative conflict record.
type ScratchpadErrorDetails struct {
	Scratchpad *Scratchpad `json:"scratchpad,omitempty"`
}

// Validate checks typed scratchpad error details from a remote boundary.
func (details ScratchpadErrorDetails) Validate(code ScratchpadErrorCode) error {
	if code == ScratchpadRevisionConflict {
		if details.Scratchpad == nil {
			return fmt.Errorf("scratchpad conflict details are missing")
		}
		return details.Scratchpad.Validate()
	}
	if details.Scratchpad != nil {
		return fmt.Errorf("scratchpad details are invalid for %q", code)
	}
	return nil
}

// Validate checks a stable scratchpad error code.
func (code ScratchpadErrorCode) Validate() error {
	switch code {
	case ScratchpadInvalidContent, ScratchpadTooLarge, ScratchpadRevisionConflict, ScratchpadRevisionExhausted,
		ScratchpadMigrationRequired, ScratchpadUnsupported, ScratchpadUnavailable:
		return nil
	default:
		return fmt.Errorf("scratchpad error code %q is invalid", code)
	}
}
