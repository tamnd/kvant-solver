package corpus

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Validate reports what is wrong with a page's front matter.
func (f *PageFront) Validate() error {
	var errs []error
	key, err := ParseIssueKey(f.Issue)
	if err != nil {
		errs = append(errs, err)
	} else {
		if f.Year != key.Year {
			errs = append(errs, fmt.Errorf("year %d does not match issue %s", f.Year, f.Issue))
		}
		if f.Number != key.Number {
			errs = append(errs, fmt.Errorf("number %q does not match issue %s", f.Number, f.Issue))
		}
	}
	if f.PageIndex < 1 {
		errs = append(errs, fmt.Errorf("page_index %d is not positive", f.PageIndex))
	}
	errs = append(errs, f.validateProvenance()...)
	return errors.Join(errs...)
}

// Validate reports what is wrong with an article's front matter.
func (f *ArticleFront) Validate() error {
	var errs []error
	if strings.TrimSpace(f.ID) == "" {
		errs = append(errs, errors.New("id is empty"))
	}
	if _, err := ParseIssueKey(f.Issue); err != nil {
		errs = append(errs, err)
	}
	if strings.TrimSpace(f.Title) == "" {
		errs = append(errs, errors.New("title is empty"))
	}
	if f.PageFirst < 1 {
		errs = append(errs, fmt.Errorf("page_first %d is not positive", f.PageFirst))
	}
	if f.PageLast < f.PageFirst {
		errs = append(errs, fmt.Errorf("page_last %d is before page_first %d", f.PageLast, f.PageFirst))
	}
	if f.Tag != "" && !f.Tag.Valid() {
		errs = append(errs, fmt.Errorf("tag %q is not four characters of 0-9 and A-Z", f.Tag))
	}
	errs = append(errs, f.validateProvenance()...)
	return errors.Join(errs...)
}

// Validate reports what is wrong with a problem's front matter.
func (f *ProblemFront) Validate() error {
	var errs []error
	id, err := ParseProblemID(f.ID)
	if err != nil {
		errs = append(errs, err)
	} else if id.Subject != f.Subject {
		errs = append(errs, fmt.Errorf("subject %q does not match id %s", f.Subject, f.ID))
	}
	if _, err := ParseIssueKey(f.PosedIn); err != nil {
		errs = append(errs, fmt.Errorf("posed_in: %w", err))
	}
	if f.HasPublishedSolution && f.SolvedIn == "" {
		errs = append(errs, errors.New("has_published_solution is true but solved_in is empty"))
	}
	if f.SolvedIn != "" {
		if _, err := ParseIssueKey(f.SolvedIn); err != nil {
			errs = append(errs, fmt.Errorf("solved_in: %w", err))
		}
	}
	if f.Tag != "" && !f.Tag.Valid() {
		errs = append(errs, fmt.Errorf("tag %q is not four characters of 0-9 and A-Z", f.Tag))
	}
	errs = append(errs, f.validateProvenance()...)
	return errors.Join(errs...)
}

// Validate reports what is wrong with a solution's front matter.
func (f *SolutionFront) Validate() error {
	var errs []error
	if _, err := ParseProblemID(f.ID); err != nil {
		errs = append(errs, err)
	}
	if f.GradedAgainstPublished && f.Agreement == "" {
		errs = append(errs, errors.New("graded_against_published is true but agreement is empty"))
	}
	errs = append(errs, f.validateProvenance()...)
	return errors.Join(errs...)
}

var knownExtractions = []string{ExtractionNative, ExtractionPublisher, ExtractionVision}

func (p *Provenance) validateProvenance() []error {
	var errs []error
	if strings.TrimSpace(p.Lang) == "" {
		errs = append(errs, errors.New("lang is empty"))
	}
	if p.Extraction != "" && !slices.Contains(knownExtractions, p.Extraction) {
		errs = append(errs, fmt.Errorf("extraction %q is not one of %s", p.Extraction, strings.Join(knownExtractions, ", ")))
	}
	if len(p.ContentSHA256) != 64 {
		errs = append(errs, fmt.Errorf("content_sha256 %q is not a sha256", p.ContentSHA256))
	}
	return errs
}

// Stale reports whether a translation no longer matches what it was made from.
// It takes the current hash of the source body, the digest of the glossary rows
// that source mentions today, and the hash of the prompt in use, and answers
// without a model call. An empty expectation means that test does not apply,
// which is how a file written before a field existed stays usable.
func (t Translated) Stale(sourceHash, glossaryTerms, promptHash, recordedPrompt string) (bool, string) {
	if t.TranslatedFrom == "" {
		return false, ""
	}
	if t.SourceSHA256 != "" && sourceHash != "" && t.SourceSHA256 != sourceHash {
		return true, "source body has changed"
	}
	if t.GlossaryTerms != "" && glossaryTerms != "" && t.GlossaryTerms != glossaryTerms {
		return true, "glossary terms this file was shown have moved"
	}
	if recordedPrompt != "" && promptHash != "" && recordedPrompt != promptHash {
		return true, "translation prompt has been edited"
	}
	return false, ""
}
