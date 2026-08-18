package corpus

// Every content file is Markdown under a YAML block fenced by three dashes,
// the same shape taocp and bourbaki use. Four schemas live in that block: a
// page, an article, a problem and a solution. A translation is not a fifth
// schema, it is one of those four with the provenance of the translation added,
// because keeping them apart would mean two definitions of the same twenty
// fields drifting from each other.

// Provenance is on every file. It answers where the text came from, what made
// it, and what it hashed to when it was written.
type Provenance struct {
	Lang            string `yaml:"lang"`
	Source          string `yaml:"source,omitempty"`
	SourceID        string `yaml:"source_id,omitempty"`
	SourceScan      string `yaml:"source_scan,omitempty"`
	Extraction      string `yaml:"extraction,omitempty"`
	ExtractionModel string `yaml:"extraction_model,omitempty"`
	ContentSHA256   string `yaml:"content_sha256"`
	PromptSHA256    string `yaml:"prompt_sha256,omitempty"`
}

// The three extraction paths. Which one a page takes is measured by textguard
// before any model call is made, and recorded here so that the cost of a file
// is readable after the fact.
const (
	ExtractionNative    = "native"    // the issue PDF is born digital
	ExtractionPublisher = "publisher" // the archive already carried the text
	ExtractionVision    = "vision"    // the page image went through OCR
)

// Translated is added to a file that is a translation of another. Staleness is
// then decidable without a model call, and it is decidable three ways: the
// source body has changed, the glossary rows this file was actually shown have
// moved, or the prompt that made it has been edited.
//
// GlossaryTerms is the digest of the rows the translator saw, not of the whole
// glossary, because the version moves for every edit anywhere and would restale
// a file over a phrase that does not occur in it.
type Translated struct {
	TranslatedFrom   string `yaml:"translated_from,omitempty"`
	SourceSHA256     string `yaml:"source_content_sha256,omitempty"`
	TranslationModel string `yaml:"translation_model,omitempty"`
	TranslationRun   string `yaml:"translation_run,omitempty"`
	GlossaryVersion  int    `yaml:"glossary_version,omitempty"`
	GlossaryTerms    string `yaml:"glossary_terms_sha256,omitempty"`
}

// PageFront heads content/<lang>/<year>/<issue>/pages/NNNN.md, which is the
// ground truth of the corpus. Articles are assembled out of these, and anything
// no article claims survives only here.
type PageFront struct {
	Issue      string   `yaml:"issue"`
	Year       int      `yaml:"year"`
	Number     string   `yaml:"number"`
	PageIndex  int      `yaml:"page_index"`
	PageLabel  string   `yaml:"page_label,omitempty"`
	Rubrics    []string `yaml:"rubrics,omitempty"`
	Articles   []string `yaml:"articles,omitempty"`
	Illegible  int      `yaml:"illegible,omitempty"`
	Provenance `yaml:",inline"`
	Translated `yaml:",inline"`
}

// ArticleFront heads content/<lang>/<year>/<issue>/articles/NN_slug.md.
type ArticleFront struct {
	ID         string   `yaml:"id"`
	Issue      string   `yaml:"issue"`
	Year       int      `yaml:"year"`
	Number     string   `yaml:"number"`
	Title      string   `yaml:"title"`
	Authors    []string `yaml:"authors,omitempty"`
	Rubric     string   `yaml:"rubric,omitempty"`
	RubricSub  string   `yaml:"rubric_sub,omitempty"`
	PageFirst  int      `yaml:"page_first"`
	PageLast   int      `yaml:"page_last"`
	PageLabels string   `yaml:"page_labels,omitempty"`
	Tag        Tag      `yaml:"tag,omitempty"`
	Statements int      `yaml:"statements,omitempty"`
	Problems   []string `yaml:"problems,omitempty"`
	Provenance `yaml:",inline"`
	Translated `yaml:",inline"`
}

// ProblemFront heads content/<lang>/problems/{m,f}/NNNN.md.
//
// PosedIn and SolvedIn being different issues is the normal case, not an edge
// one. The magazine prints a problem and then prints its solution two to four
// issues later, which is why the problem corpus cannot be assembled one issue
// at a time and why these two fields exist rather than a single issue field.
type ProblemFront struct {
	ID                   string   `yaml:"id"`
	Subject              Subject  `yaml:"subject"`
	Tag                  Tag      `yaml:"tag,omitempty"`
	PosedIn              string   `yaml:"posed_in"`
	PosedPages           string   `yaml:"posed_pages,omitempty"`
	SolvedIn             string   `yaml:"solved_in,omitempty"`
	SolvedPages          string   `yaml:"solved_pages,omitempty"`
	Authors              []string `yaml:"authors,omitempty"`
	HasPublishedSolution bool     `yaml:"has_published_solution"`
	Provenance           `yaml:",inline"`
	Translated           `yaml:",inline"`
}

// SolutionFront heads content/solutions/<lang>/problems/{m,f}/NNNN.md, which is
// our worked solution and not the one the magazine printed.
type SolutionFront struct {
	ID                     string  `yaml:"id"`
	Subject                Subject `yaml:"subject"`
	Tag                    Tag     `yaml:"tag,omitempty"`
	SolvedBy               string  `yaml:"solved_by,omitempty"`
	SolverRun              string  `yaml:"solver_run,omitempty"`
	Verified               bool    `yaml:"verified"`
	GradedAgainstPublished bool    `yaml:"graded_against_published,omitempty"`
	Agreement              string  `yaml:"agreement,omitempty"`
	// RightAnswer is which of the two the grader believed where they differ. It
	// is separate from Agreement because that one is the mark against what the
	// magazine printed, and the magazine printed corrections to itself.
	RightAnswer string `yaml:"right_answer,omitempty"`
	// Anachronism is a method used here that the magazine did not have when it
	// set the problem. It is not a fault and it does not change the mark.
	Anachronism string `yaml:"anachronism,omitempty"`
	Provenance  `yaml:",inline"`
	Translated  `yaml:",inline"`
}

// Front is what the loader and the writer need from any of the four schemas.
type Front interface {
	// ContentHash returns what the file recorded when it was written.
	ContentHash() string
	// SetContentHash is called by Save with the hash of the body it is about
	// to write, so that no caller has to remember to do it.
	SetContentHash(string)
	// Validate reports what is wrong with the front matter itself, before
	// anything is compared against the body or the rest of the corpus.
	Validate() error
}

// ContentHash implements Front for every schema through the embedded struct.
func (p *Provenance) ContentHash() string { return p.ContentSHA256 }

// SetContentHash implements Front.
func (p *Provenance) SetContentHash(h string) { p.ContentSHA256 = h }

// PageID is the identifier this page front matter describes.
func (f *PageFront) PageID() PageID {
	key, err := ParseIssueKey(f.Issue)
	if err != nil {
		key = IssueKey{Year: f.Year, Number: f.Number}
	}
	return PageID{Issue: key, Index: f.PageIndex}
}

// Filename is what this page is called inside its issue directory.
func (f *PageFront) Filename() string { return f.PageID().Filename() }

// ProblemID is the identifier this problem front matter describes.
func (f *ProblemFront) ProblemID() (ProblemID, error) { return ParseProblemID(f.ID) }
