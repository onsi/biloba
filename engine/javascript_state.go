package engine

import (
	"context"
	"encoding/json"
)

func (s *Session) WaitForDefined(ctx context.Context, expression string, policy PollPolicy) (PollResult, error) {
	script := "(() => { const value = (" + expression + "); return {defined: value !== undefined, value}; })()"
	return Poll(ctx, policy, func(attemptCtx context.Context) (Observation, bool, error) {
		var envelope struct {
			Defined bool            `json:"defined"`
			Value   json.RawMessage `json:"value"`
		}
		err := s.serial(attemptCtx, "wait for defined JavaScript value", func(opCtx context.Context) error {
			return EvaluateContext(opCtx, script, false, &envelope)
		})
		if err != nil {
			return Observation{}, false, err
		}
		if !envelope.Defined {
			return Observation{}, false, nil
		}
		var value any
		if len(envelope.Value) > 0 {
			if err := json.Unmarshal(envelope.Value, &value); err != nil {
				return Observation{}, false, Fatal(err)
			}
		}
		return Observation{Value: value}, true, nil
	})
}
