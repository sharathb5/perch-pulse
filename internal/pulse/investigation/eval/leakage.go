package eval

import (
	"encoding/json"
	"fmt"
	"strings"
)

// AssertNoAnnotationLeakage fails if evaluator annotations or hidden ground truth
// appear in the generation-only JSON payload.
func AssertNoAnnotationLeakage(c Case) error {
	gen, err := json.Marshal(c.GenerationOnly())
	if err != nil {
		return err
	}
	raw := strings.ToLower(string(gen))
	if c.HiddenGroundTruth != nil {
		for _, secret := range []string{
			c.HiddenGroundTruth.ScenarioID,
			c.HiddenGroundTruth.InjectedFault,
			c.HiddenGroundTruth.LabelNote,
		} {
			secret = strings.TrimSpace(secret)
			if secret == "" {
				continue
			}
			if strings.Contains(raw, strings.ToLower(secret)) {
				return fmt.Errorf("eval: hidden ground truth %q leaked into generation input", secret)
			}
		}
		// TrueService may coincide with affected service ids; only reject when prefixed as GT.
		if ts := strings.TrimSpace(c.HiddenGroundTruth.TrueService); ts != "" {
			if strings.HasPrefix(strings.ToLower(ts), "gt:") && strings.Contains(raw, strings.ToLower(ts)) {
				return fmt.Errorf("eval: hidden ground truth service %q leaked into generation input", ts)
			}
		}
		// Explicit marker fields must never serialize into generation.
		if strings.Contains(raw, "hidden_ground_truth") || strings.Contains(raw, "injected_fault") {
			return fmt.Errorf("eval: hidden_ground_truth field leaked into generation JSON")
		}
	}
	if strings.Contains(raw, `"annotations"`) || strings.Contains(raw, "human_rubric_hints") {
		return fmt.Errorf("eval: annotations leaked into generation JSON")
	}
	return nil
}
