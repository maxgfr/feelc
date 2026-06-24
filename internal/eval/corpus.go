package eval

// Corpus is the frozen set of authoring challenges used to measure rule-authoring quality. Each Task's
// cases are the objective bar a produced model must clear. The corpus is solution-AGNOSTIC: any model
// that compiles, verifies clean, and reproduces the cases scores 100%, regardless of how it was written
// — so it fairly measures an LLM (or a human) without prescribing the answer.
var Corpus = []Task{
	{
		Name:   "tiered-discount",
		Prompt: "Author a model with a number input `amount` and a decision `discount` (number): if amount ≥ 500 the discount is 15, if ≥ 100 it is 10, if ≥ 50 it is 5, otherwise 0. First match wins.",
		Cases: []Case{
			{Decision: "discount", Input: map[string]any{"amount": 600}, Expect: "15"},
			{Decision: "discount", Input: map[string]any{"amount": 150}, Expect: "10"},
			{Decision: "discount", Input: map[string]any{"amount": 60}, Expect: "5"},
			{Decision: "discount", Input: map[string]any{"amount": 10}, Expect: "0"},
		},
	},
	{
		Name:   "risk-band",
		Prompt: "Author a model with a number input `score` and a decision `band` (string): score ≥ 700 → \"high\", ≥ 400 → \"medium\", otherwise \"low\". First match wins.",
		Cases: []Case{
			{Decision: "band", Input: map[string]any{"score": 800}, Expect: "high"},
			{Decision: "band", Input: map[string]any{"score": 500}, Expect: "medium"},
			{Decision: "band", Input: map[string]any{"score": 100}, Expect: "low"},
		},
	},
}
