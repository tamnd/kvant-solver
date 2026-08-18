package pdfsrc

import (
	"context"
	"strings"
	"testing"
)

// Real output, trimmed: printed page 64 of the January 2010 issue, its running
// head and the first two lines of the left column. The masthead words are here
// because the logo face gives the last letter of Квант as a Latin T and puts a
// bare dollar after it, and everything downstream has to see that the way the
// file actually has it.
const layout2010 = `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd"><html xmlns="http://www.w3.org/1999/xhtml">
<head>
<title>2010-01.pdf</title>
<meta name="Author" content="mpanov"/>
</head>
<body>
<doc>
  <page width="659.000000" height="859.000000">
    <flow>
      <block xMin="282.337000" yMin="64.352500" xMax="380.783633" yMax="74.270000">
        <line xMin="282.337000" yMin="64.352500" xMax="380.783633" yMax="74.270000">
          <word xMin="282.337000" yMin="64.352500" xMax="321.504917" yMax="72.835000">&#1050;&#1042;&#1040;&#1053;T</word>
          <word xMin="324.240900" yMin="68.726000" xMax="328.864900" yMax="74.270000">$</word>
          <word xMin="335.280700" yMin="66.325000" xMax="380.783633" yMax="72.940000">2010/&#8470;1</word>
        </line>
      </block>
    </flow>
    <flow>
      <block xMin="65.616700" yMin="65.803230" xMax="77.720980" yMax="74.279230">
        <line xMin="65.616700" yMin="65.803230" xMax="77.720980" yMax="74.279230">
          <word xMin="65.616700" yMin="65.803230" xMax="77.720980" yMax="74.279230">64</word>
        </line>
      </block>
      <block xMin="65.000000" yMin="120.000000" xMax="300.000000" yMax="140.000000">
        <line xMin="65.000000" yMin="120.000000" xMax="300.000000" yMax="130.000000">
          <word xMin="65.000000" yMin="120.000000" xMax="80.000000" yMax="129.000000">&#1058;&#1086;&#1075;&#1076;&#1072;</word>
          <word xMin="84.000000" yMin="120.000000" xMax="92.000000" yMax="129.000000">S</word>
          <word xMin="92.000000" yMin="124.000000" xMax="100.000000" yMax="130.000000">KLM</word>
          <word xMin="104.000000" yMin="120.000000" xMax="112.000000" yMax="129.000000">&#8805;</word>
          <word xMin="116.000000" yMin="120.000000" xMax="124.000000" yMax="129.000000">2</word>
          <word xMin="124.000000" yMin="117.000000" xMax="130.000000" yMax="123.000000">k</word>
          <word xMin="134.000000" yMin="120.000000" xMax="140.000000" yMax="129.000000">.</word>
          <word xMin="150.000000" yMin="120.000000" xMax="160.000000" yMax="129.000000"></word>
        </line>
        <line xMin="65.000000" yMin="132.000000" xMax="300.000000" yMax="141.000000">
          <word xMin="65.000000" yMin="132.000000" xMax="90.000000" yMax="141.000000">&#1054;&#1073;&#1072;</word>
          <word xMin="94.000000" yMin="132.000000" xMax="150.000000" yMax="141.000000">&#1085;&#1077;&#1088;&#1072;&#1074;&#1077;&#1085;&#1089;&#1090;&#1074;&#1072;</word>
          <word xMin="154.000000" yMin="132.000000" xMax="200.000000" yMax="141.000000">&#1089;&#1083;&#1077;&#1076;&#1091;&#1102;&#1090;</word>
          <word xMin="204.000000" yMin="132.000000" xMax="230.000000" yMax="141.000000">&#1080;&#1079;</word>
        </line>
      </block>
    </flow>
  </page>
</doc>
</body>
</html>
`

func TestTheGeometryOfEveryWordSurvivesTheParse(t *testing.T) {
	pages, err := ParseLayout(strings.NewReader(layout2010))
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 1 {
		t.Fatalf("%d pages, want 1", len(pages))
	}
	page := pages[0]
	if page.Number != 1 || page.Width != 659 || page.Height != 859 {
		t.Fatalf("page %d came out %v by %v", page.Number, page.Width, page.Height)
	}
	if len(page.Blocks) != 3 {
		t.Fatalf("%d blocks, want the running head, the folio and the paragraph", len(page.Blocks))
	}
	head := page.Blocks[0].Lines[0].Words
	if len(head) != 3 {
		t.Fatalf("the running head has %d words: %+v", len(head), head)
	}
	if head[0].Text != "КВАНT" {
		t.Errorf("the masthead reads %q, and the Latin T in it is the file's own", head[0].Text)
	}
	if got := head[0].Width(); got < 39 || got > 40 {
		t.Errorf("the first word is %v wide, want about 39 points", got)
	}
}

func TestAWordWithNoTextIsDropped(t *testing.T) {
	// pdftotext emits a box around a glyph it has no character for, and an empty
	// token in the middle of a formula is a formula that does not parse.
	pages, err := ParseLayout(strings.NewReader(layout2010))
	if err != nil {
		t.Fatal(err)
	}
	for _, block := range pages[0].Blocks {
		for _, line := range block.Lines {
			for _, w := range line.Words {
				if w.Text == "" {
					t.Fatal("an empty word came through the parse")
				}
			}
		}
	}
}

func TestTheBodySizeIsWhatMostOfThePageIsSetIn(t *testing.T) {
	pages, _ := ParseLayout(strings.NewReader(layout2010))
	// Most words on this fixture are 9 points tall, and the median has to say so
	// rather than being pulled about by the two scripts.
	if got := pages[0].BodyHeight(); got != 9 {
		t.Fatalf("the body is %v points, want 9", got)
	}
	line := pages[0].Blocks[2].Lines[0]
	if got := line.Base(); got != 129 {
		t.Fatalf("the baseline is %v, want the 129 most of the line sits on", got)
	}
}

func TestAPageRangeIsRenumberedOntoTheFile(t *testing.T) {
	// pdftotext numbers what it printed from one, and every other part of this
	// project counts pages of the file.
	run := &FakeRunner{Out: map[string]string{
		"pdftotext -bbox-layout -f 66 -l 66 /tmp/kvant_2010_1.pdf -": layout2010,
	}}
	pages, err := (&Source{Path: "/tmp/kvant_2010_1.pdf", Run: run}).Layout(context.Background(), 66, 66)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 1 || pages[0].Number != 66 {
		t.Fatalf("the page came back as number %d, want 66", pages[0].Number)
	}
}

func TestAControlCharacterInTheTextLayerDoesNotStopTheIssue(t *testing.T) {
	// A few of these files carry a group separator where a letter should be,
	// left behind by an encoding that was wrong before the issue went to press.
	// XML cannot hold one, and the character is not on the paper: 2017 issue 6
	// died on line 3819 of its own layout over exactly this.
	bad := strings.Replace(layout2010, ">64<", ">6\x1d4<", 1)
	pages, err := ParseLayout(strings.NewReader(bad))
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 1 {
		t.Fatalf("%d pages came back", len(pages))
	}
	folio := pages[0].Blocks[1].Lines[0].Words[0]
	if folio.Text != "64" {
		t.Fatalf("the page number came out as %q", folio.Text)
	}
}
