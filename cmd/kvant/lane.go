package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/api"
	"github.com/tamnd/kvant-solver/fleet"
	"github.com/tamnd/kvant-solver/ocr"
	"github.com/tamnd/kvant-solver/prompt"
)

// laneFlags are the choice of what reads a page. Two commands take them, ocr
// and repair, and they have to agree down to the prompt hash: a page read by
// the repair lane and a page read by the first lane are the same page in the
// same corpus, and the front matter has to be able to say which model produced
// which.
type laneFlags struct {
	kind     *string
	endpoint *string
	key      *string
	model    *string
	short    *string
	command  *string
	hosts    *[]string
	tool     *string
	display  *string
	timeout  *time.Duration
	fs       *pflag.FlagSet
}

// FleetModel is what the rented boxes read a page with. The tool drives a
// browser signed in to a real account, so the model is whatever that account is
// served, and naming it here is how the corpus records it.
const FleetModel = "gpt-5"

// FleetTool is where chatgpt-tool lives on server2 and server3. It is under
// /home/tam on server1, which is what --tool is for.
const FleetTool = "/root/chatgpt-tool/.venv/bin/chatgpt-tool"

// addLaneFlags puts the engine flags on a command. The defaults name the card,
// because that is the lane a decade is read with; repair overrides them.
func addLaneFlags(fs *pflag.FlagSet, kind string) laneFlags {
	return laneFlags{
		// The endpoint is the whole of the engine choice for the served lane.
		// chatgpt-tool serve and vllm serve answer the same route, so the
		// difference between the browser lane and the card in the other room is a
		// URL and a model name.
		kind:     fs.String("engine", kind, "served for an endpoint, cli for a local program, fleet for the rented boxes"),
		endpoint: fs.String("api", envOr("KVANT_API", "http://127.0.0.1:8000/v1/chat/completions"), "an OpenAI shaped endpoint that takes an image"),
		key:      fs.String("api-key", os.Getenv("KVANT_API_KEY"), "bearer token for the endpoint, when it wants one"),
		model:    fs.String("model", envOr("KVANT_MODEL", "zai-org/GLM-OCR"), "the model to ask"),
		// A document model wants one sentence and a general model wants the whole
		// page prompt, and sending the wrong one to either costs both speed and
		// accuracy.
		short:   fs.String("short-prompt", "auto", "send the one sentence prompt a document model wants: auto, yes or no"),
		command: fs.String("cli", envOr("KVANT_CLI", "claude -p --allowedTools Read"), "the program the cli engine runs"),
		// The hosts are ssh names out of the user's own config and not addresses,
		// because which user and which key reaches which box is that file's
		// business and not this repo's.
		hosts:   fs.StringSlice("host", nil, "an ssh name of a box the fleet engine reads on, repeatable"),
		tool:    fs.String("tool", envOr("KVANT_TOOL", FleetTool), "where chatgpt-tool lives on those boxes"),
		display: fs.String("display", ocr.DefaultDisplay, "the X display Chrome opens on there"),
		timeout: fs.Duration("timeout", ocr.DefaultPageTimeout, "how long one page may take"),
		fs:      fs,
	}
}

// lane is a built engine and everything that has to be recorded about it.
type lane struct {
	Engine ocr.Engine
	// Folio reads the printed number off the foot of a sheet for the lanes that
	// do not answer the question themselves.
	Folio  *ocr.Folioer
	Prompt string
	SHA256 string
}

// build turns the flags into an engine.
func (f laneFlags) build() (lane, error) {
	// auto is the served lane short and the cli lane long, which is what the
	// measurements say and what somebody means when they do not say. The one
	// sentence version exists to stop a recogniser drowning in instructions, and
	// a model that follows instructions should be given them: it is the only
	// lane that can answer the folio question by itself.
	short := *f.kind == "served"
	switch *f.short {
	case "yes", "true":
		short = true
	case "no", "false":
		short = false
	case "auto":
	default:
		return lane{}, fmt.Errorf("--short-prompt is auto, yes or no, not %q", *f.short)
	}

	built := lane{Prompt: prompt.OCRPage(), SHA256: prompt.OCRPageSHA256()}
	if short {
		built.Prompt, built.SHA256 = prompt.OCRShort(), prompt.OCRShortSHA256()
	}

	switch *f.kind {
	case "served":
		client := &api.Client{
			URL:        *f.endpoint,
			APIKey:     *f.key,
			HTTPClient: &http.Client{Timeout: *f.timeout + time.Minute},
			UserAgent:  "kvant/" + version,
		}
		built.Engine = ocr.Served{Client: client, Model: *f.model, Prompt: built.Prompt, Timeout: *f.timeout}
		// The folio band is only read for the lane that needs it. A general model
		// answers the folio question in the page prompt, and reading a corner of
		// the same page a second time to check it would be one more call for less
		// evidence.
		if short {
			built.Folio = &ocr.Folioer{Engine: ocr.Served{
				Client:  client,
				Model:   *f.model,
				Prompt:  prompt.OCRBand(),
				Timeout: *f.timeout,
			}}
		}
	case "cli":
		fields := strings.Fields(*f.command)
		if len(fields) == 0 {
			return lane{}, fmt.Errorf("--cli names no program")
		}
		// The default --model names the card's model, which did not read this
		// page, so it is only believed here when it was asked for.
		name := fields[0]
		if f.fs.Changed("model") {
			name = *f.model
		}
		built.Engine = ocr.Command{
			Model:   name,
			Path:    fields[0],
			Args:    fields[1:],
			Prompt:  built.Prompt,
			Timeout: *f.timeout,
		}
	case "fleet":
		if len(*f.hosts) == 0 {
			return lane{}, fmt.Errorf("--engine fleet needs at least one --host")
		}
		// One ssh per command and a generous bound on it. The command being timed
		// is a browser opening a page, uploading a picture and waiting for a model
		// to read it, which is minutes rather than seconds, and cutting it short
		// spends the account's upload for nothing.
		shell := fleet.SSH{Timeout: *f.timeout + 2*time.Minute}
		copier := ocr.Rsync{Timeout: *f.timeout}
		name := FleetModel
		if f.fs.Changed("model") {
			name = *f.model
		}
		lanes := make([]ocr.Engine, 0, len(*f.hosts))
		for _, host := range *f.hosts {
			lanes = append(lanes, &ocr.Remote{
				Host: ocr.Host{
					Name: host, Tool: *f.tool, Display: *f.display,
				},
				Shell: shell, Copy: copier,
				Prompt: built.Prompt, Model: name, Timeout: *f.timeout,
			})
		}
		built.Engine = &ocr.Pool{Lanes: lanes}
	default:
		return lane{}, fmt.Errorf("--engine is served, cli or fleet, not %q", *f.kind)
	}
	return built, nil
}
