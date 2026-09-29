package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jstevewhite/snp/internal/ai"
	"github.com/jstevewhite/snp/internal/ask"
	"github.com/jstevewhite/snp/internal/config"
	"github.com/jstevewhite/snp/internal/edit"
)

// runAsk is `snp ask`: one AI generation (spec §13) from a prompt —
// every positional argument joined by spaces. stdout is the body
// exactly, capture-pure like pick; `--add` opens the create panel on
// the result instead and prints nothing.
func runAsk(args []string) {
	fs := newSubFlags("ask")
	fs.String("url", "", "remote library (http or https); overrides the url config key")
	local := fs.Bool("local", false, "use the local AI config, ignoring url")
	kind := fs.String("kind", "command", "what to generate: command, script, or function")
	language := fs.String("language", "", "language label, e.g. bash")
	add := fs.Bool("add", false, "open the editor panel on the result instead of printing it")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	prompt := strings.Join(fs.Args(), " ")
	if prompt == "" {
		usageError("usage: snp ask [--url URL | --local] [--kind command|script|function] [--language L] [--add] <prompt...>")
	}
	cfg, err := config.LoadDesktop(fs)
	if err != nil {
		fatal(err)
	}
	gen, err := askGenerate(context.Background(), cfg, *local, prompt, *kind, *language)
	if err != nil {
		fatal(err)
	}
	if !*add {
		fmt.Println(gen.Body)
		return
	}
	ed, done, err := openEditorLibrary(cfg, *local)
	if err != nil {
		fatal(err)
	}
	svc, err := openAskService(cfg, *local, editorStore(ed))
	if err != nil {
		done()
		fatal(err)
	}
	_, err = edit.RunCreate(context.Background(), ed, svc, prefillFromGeneration(gen))
	done()
	if errors.Is(err, edit.ErrCanceled) {
		os.Exit(1)
	}
	if err != nil {
		fatal(err)
	}
}

// askGenerate runs one generation over the resolved transport. The web
// client's empty-body check (askAI) lands here: a generation with no
// body is an error, not an empty print.
func askGenerate(ctx context.Context, cfg config.Config, forceLocal bool, prompt, kind, language string) (ask.Generation, error) {
	svc, err := openAskService(cfg, forceLocal, nil)
	if err != nil {
		return ask.Generation{}, err
	}
	gen, err := svc.Generate(ctx, prompt, ai.Kind(kind), language)
	if err != nil {
		return ask.Generation{}, err
	}
	if gen.Body == "" {
		return ask.Generation{}, errors.New("AI returned an empty snippet")
	}
	return gen, nil
}

// prefillFromGeneration applies the web form's fill rules (askAI):
// body always; title, language, and notes only when the model produced
// them — an empty string prefills nothing.
func prefillFromGeneration(gen ask.Generation) edit.Prefill {
	return edit.Prefill{
		Title:    gen.Title,
		Language: gen.Language,
		Body:     gen.Body,
		Notes:    gen.Notes,
	}
}
