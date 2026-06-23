package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/maxgfr/feelc/internal/check"
	"github.com/maxgfr/feelc/internal/genai"
	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/loader"
	"github.com/maxgfr/feelc/internal/verify"
)

const (
	defaultIngestRounds = 3
	maxIngestRounds     = 5
)

// DecisionSource maps a decision to the @source citation it carries. It is read from the COMPILED
// model — never parsed from the LLM's prose — so the engine stays the source of truth.
type DecisionSource struct {
	Decision   string `json:"decision"`
	SourceSpan string `json:"sourceSpan"`
}

// RepairRound audits one iteration of the bounded verify->repair loop.
type RepairRound struct {
	N        int              `json:"n"`
	Blockers int              `json:"blockers"`
	Findings []verify.Finding `json:"findings,omitempty"`
	Verdicts []check.Verdict  `json:"verdicts,omitempty"`
	Compile  string           `json:"compileError,omitempty"`
}

// ingestResult is the outcome of the loop (the /v1/ingest response).
type ingestResult struct {
	Rules     string           `json:"rules"`
	Mapping   []DecisionSource `json:"mapping"`
	Verify    *verify.Report   `json:"verify,omitempty"`
	Check     *check.Report    `json:"check,omitempty"`
	Blockers  int              `json:"blockers"`
	Rounds    []RepairRound    `json:"rounds"`
	Converged bool             `json:"converged"`
	Message   string           `json:"message,omitempty"`
}

// handleIngest is the generic AI rule-management loop (any business domain). The LLM drafts a
// `.rules` model from an arbitrary specification; the deterministic engine compiles, verifies and
// checks it and feeds findings back for a bounded number of repair rounds. The LLM only ever emits
// `.rules` text — every gate and the loop control are pure Go, and the decision->@source mapping is
// read back from the compiled model. 501 when no LLM is configured (honest degradation, like /v1/chat).
func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req struct {
		Source    string        `json:"source"`
		Claims    []check.Claim `json:"claims"`
		MaxRounds int           `json:"maxRounds"`
		LLM       genai.Config  `json:"llm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Source) == "" {
		writeErr(w, http.StatusBadRequest, "`source` required")
		return
	}
	prov, err := genai.Resolve(req.LLM)
	if errors.Is(err, genai.ErrNotConfigured) {
		writeErr(w, http.StatusNotImplemented, err.Error())
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	res, err := runIngestLoop(ctx, prov, req.Source, req.Claims, req.MaxRounds)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// runIngestLoop drives the bounded draft->verify->repair loop. It is pure control flow over the
// deterministic engine: the provider only emits text; compile/verify/check and the convergence
// decision are the engine's.
func runIngestLoop(ctx context.Context, prov genai.Provider, source string, claims []check.Claim, maxRounds int) (*ingestResult, error) {
	if maxRounds <= 0 {
		maxRounds = defaultIngestRounds
	}
	if maxRounds > maxIngestRounds {
		maxRounds = maxIngestRounds
	}
	msgs := []genai.Message{{Role: "user", Content: source}}
	res := &ingestResult{}
	for round := 1; round <= maxRounds; round++ {
		reply, err := prov.Chat(ctx, genai.IngestPrompt, msgs)
		if err != nil {
			return nil, fmt.Errorf("LLM call failed: %w", err)
		}
		res.Message = reply
		rules := extractRules(reply)
		if rules == "" {
			res.Rounds = append(res.Rounds, RepairRound{N: round, Compile: "no rules block in the assistant reply"})
			break
		}
		res.Rules = rules

		cm, _, vrep, cerr := loader.Compile([]byte(rules))
		if cerr != nil {
			rd := RepairRound{N: round, Blockers: 1, Compile: cerr.Error()}
			res.Rounds = append(res.Rounds, rd)
			res.Blockers = 1
			if round == maxRounds {
				break
			}
			msgs = appendRepair(msgs, reply, rd)
			continue
		}

		var crep *check.Report
		if len(claims) > 0 {
			crep = check.Check(cm, claims)
		}
		blockers := 0
		if vrep != nil {
			blockers += vrep.Blockers()
		}
		if crep != nil {
			blockers += crep.Blockers()
		}
		rd := RepairRound{N: round, Blockers: blockers, Findings: blockingFindings(vrep), Verdicts: blockingVerdicts(crep)}
		res.Rounds = append(res.Rounds, rd)
		res.Verify = vrep
		res.Check = crep
		res.Blockers = blockers
		res.Mapping = decisionSources(cm)

		if blockers == 0 {
			res.Converged = true
			break
		}
		if round == maxRounds {
			break
		}
		msgs = appendRepair(msgs, reply, rd)
	}
	return res, nil
}

// appendRepair extends the conversation with the assistant's draft and the engine's deterministic
// feedback for the next round.
func appendRepair(msgs []genai.Message, reply string, rd RepairRound) []genai.Message {
	return append(msgs,
		genai.Message{Role: "assistant", Content: reply},
		genai.Message{Role: "user", Content: ingestFeedback(rd)})
}

// decisionSources reads each decision's @source from the COMPILED model (the engine is the source
// of truth — we never trust the LLM's prose for the mapping).
func decisionSources(cm *ir.CompiledModel) []DecisionSource {
	out := make([]DecisionSource, 0, len(cm.Decisions))
	for i := range cm.Decisions {
		d := &cm.Decisions[i]
		out = append(out, DecisionSource{Decision: d.Name, SourceSpan: d.Meta.Source})
	}
	return out
}

func blockingFindings(rep *verify.Report) []verify.Finding {
	if rep == nil {
		return nil
	}
	var out []verify.Finding
	for _, f := range rep.Findings {
		if f.Severity == verify.SevError {
			out = append(out, f)
		}
	}
	return out
}

func blockingVerdicts(rep *check.Report) []check.Verdict {
	if rep == nil {
		return nil
	}
	var out []check.Verdict
	for _, v := range rep.Verdicts {
		if v.Status != check.Supported {
			out = append(out, v)
		}
	}
	return out
}

// ingestFeedback renders the engine's findings into a compact, deterministic instruction for the
// next LLM turn. The witnesses come straight from the verifier / checker — the engine generates
// this feedback, not the model.
func ingestFeedback(rd RepairRound) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The deterministic engine found %d blocker(s). Fix exactly these and re-emit the COMPLETE model with @source annotations:\n", rd.Blockers)
	if rd.Compile != "" {
		fmt.Fprintf(&b, "- compile error: %s\n", rd.Compile)
	}
	for _, f := range rd.Findings {
		fmt.Fprintf(&b, "- %s in decision %q: %s", f.Kind, f.Decision, f.Message)
		if len(f.Witness) > 0 {
			fmt.Fprintf(&b, " (witness: %s)", witnessString(f.Witness))
		}
		b.WriteByte('\n')
	}
	for _, v := range rd.Verdicts {
		fmt.Fprintf(&b, "- claim on %q contradicted: %s\n", v.Claim.Decision, v.Detail)
	}
	b.WriteString("Return only the updated model in one ```rules block.")
	return b.String()
}

func witnessString(w map[string]string) string {
	keys := make([]string, 0, len(w))
	for k := range w {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + w[k]
	}
	return strings.Join(parts, ", ")
}
