package publish

import (
	"fmt"
	"strings"

	"github.com/tamnd/kvant-solver/rubric"
)

// The words the site itself says, in every language it is published in.
//
// The corpus is translated and the frame round it has to be too: an English
// reader who lands on an English article and finds the navigation in Russian
// has been told the site is not for them. Every string the templates print is
// a field here, so a language is either complete or it does not compile, and a
// language the table does not know falls back to English rather than to
// Russian, since English is the language a reader of an unknown one is likelier
// to have.

// words is the frame in one language.
type words struct {
	// Name is the language in itself, for the switcher.
	Name string
	// Brand is the magazine's name as this language writes it.
	Brand   string
	Tagline string
	About   string

	Archive, Rubrics, Authors, Problems, Tags, Search string
	SearchHint, SearchNoJS, SearchEmpty, SearchNone   string
	Menu, Theme, Contents, OnThisPage, BackToTop      string
	Skip, Languages, Home                             string

	Articles, Pages, AllPages, ReadPages string
	NoArticles, NoPages, Empty           string
	Unnumbered                           string
	Prev, Next, PrevIssue, NextIssue     string
	PrevYear, NextYear                   string
	Issue, Year                          string

	Original, Translation, Transcribed, NativeText string
	NotChecked                                     string
	// Draft is the warning on a rough automatic pass, and DraftShort is the
	// same said in a few words for the line under a sheet.
	Draft, DraftShort                      string
	Tag, TagLede, TagIndexLede, TagLookup  string
	RubricsLede, AuthorsLede, ProblemsLede string
	SignedAlso                             string
	Posed, Solved, NoSolution              string
	// Math and Physics head the two series on the problem index, WithSolution
	// is the legend of a tile whose solution was printed, and InOriginal says
	// that a tile opens the problem in the language it was printed in.
	Math, Physics, WithSolution, InOriginal string
	ChooseLanguage, NotFound, NotFoundLede  string
	Source                                  string

	// issue and friends are formats rather than words, because the order of
	// the parts is not the same in every language.
	issue, year, sheet, page, problem, tagOn string
	// count says how many of something there are, which Russian needs three
	// forms for, English two and Vietnamese one.
	count func(n int, what string) string
}

func (w words) IssueTitle(year int, number string) string { return fmt.Sprintf(w.issue, year, number) }
func (w words) YearTitle(year int) string                 { return fmt.Sprintf(w.year, year) }
func (w words) SheetTitle(index int) string               { return fmt.Sprintf(w.sheet, index) }
func (w words) PageLabel(label string) string             { return fmt.Sprintf(w.page, label) }
func (w words) ProblemTitle(id string) string             { return fmt.Sprintf(w.problem, id) }
func (w words) TagsOn(first string) string                { return fmt.Sprintf(w.tagOn, first) }
func (w words) Count(n int, what string) string           { return w.count(n, what) }

// languages is every language the frame is written in.
var languages = map[string]words{
	"ru": {
		Name:    "Русский",
		Brand:   "Квант",
		Tagline: "Научно-популярный физико-математический журнал",
		About:   "Все страницы журнала с 1970 года: статьи, задачник, олимпиады и всё, что было напечатано между ними. Текст набран заново, формулы свёрстаны, у каждого материала есть постоянный адрес.",

		Archive: "Архив", Rubrics: "Рубрики", Authors: "Авторы", Problems: "Задачник", Tags: "Теги", Search: "Поиск",
		SearchHint:  "Название, автор, рубрика или тег",
		SearchNoJS:  "Поиску нужен JavaScript. Без него весь архив доступен через рубрики, авторов и номера.",
		SearchEmpty: "Начните вводить запрос.",
		SearchNone:  "Ничего не найдено.",
		Menu:        "Меню", Theme: "Светлая или тёмная тема", Contents: "Содержание", OnThisPage: "На странице", BackToTop: "Наверх",
		Skip: "К тексту", Languages: "Язык", Home: "Главная",

		Articles: "Статьи", Pages: "Страницы", AllPages: "Все страницы номера", ReadPages: "Страницы в номере",
		NoArticles: "Номер ещё не разобран на статьи.", NoPages: "Ни одна страница ещё не прочитана.", Empty: "Пока пусто.",
		Unnumbered: "без номера",
		Prev:       "Назад", Next: "Дальше", PrevIssue: "Предыдущий номер", NextIssue: "Следующий номер",
		PrevYear: "Предыдущий год", NextYear: "Следующий год",
		Issue: "Номер", Year: "Год",

		Original:    "Оригинал",
		Translation: "Машинный перевод с русского",
		Transcribed: "Набрано по скану",
		NativeText:  "Взято из текстового слоя PDF",
		NotChecked:  "Не проверено человеком.",
		Draft:       "Черновой автоматический перевод низкого качества: возможны ошибки и неточности. Он будет заменён более качественным переводом.",
		DraftShort:  "Черновой перевод, низкое качество",
		Tag:         "Тег",
		TagLede:     "Постоянный адрес. Он закреплён за материалом навсегда, даже если тот сменит название или путь.",
		TagIndexLede: "У каждого материала есть тег из четырёх знаков. " +
			"Он закреплён навсегда и не меняется, когда материал переименован или перемещён.",
		TagLookup:   "Введите тег",
		RubricsLede: "Постоянные разделы журнала.",
		AuthorsLede: "Имена так, как они напечатаны над статьями.",
		Math:        "Математика", Physics: "Физика", WithSolution: "решение напечатано", InOriginal: "открывается на языке оригинала",
		ProblemsLede: "Задачи M и Ф под своими номерами. Условие и решение выходили в разных номерах, поэтому задача стоит отдельно от них обоих.",
		SignedAlso:   "Подписано также",
		Posed:        "Условие", Solved: "Решение", NoSolution: "решение не напечатано",
		ChooseLanguage: "Выберите язык",
		NotFound:       "Страница не найдена",
		NotFoundLede:   "Такой страницы нет. Возможно, материал ещё не прочитан.",
		Source:         "Исходники",

		issue: "Квант %d, №%s", year: "Квант %d", sheet: "Лист %d", page: "с. %s", problem: "Задача %s", tagOn: "Теги на %s",
		count: func(n int, what string) string {
			forms := map[string][3]string{
				"article": {"материал", "материала", "материалов"},
				"issue":   {"номер", "номера", "номеров"},
				"page":    {"страница", "страницы", "страниц"},
				"tag":     {"тег", "тега", "тегов"},
				"problem": {"задача", "задачи", "задач"},
				"author":  {"автор", "автора", "авторов"},
			}[what]
			return plural(n, forms[0], forms[1], forms[2])
		},
	},
	"en": {
		Name:    "English",
		Brand:   "Kvant",
		Tagline: "The popular physics and mathematics magazine",
		About:   "Every page of the magazine since 1970: the articles, the problem section, the olympiads and everything printed between them. Retypeset, with the formulas set and a permanent address for every piece.",

		Archive: "Archive", Rubrics: "Sections", Authors: "Authors", Problems: "Problems", Tags: "Tags", Search: "Search",
		SearchHint:  "Title, author, section or tag",
		SearchNoJS:  "Search needs JavaScript. Without it the whole archive is reachable through the sections, the authors and the issues.",
		SearchEmpty: "Start typing to search.",
		SearchNone:  "Nothing found.",
		Menu:        "Menu", Theme: "Light or dark", Contents: "Contents", OnThisPage: "On this page", BackToTop: "Back to top",
		Skip: "Skip to the text", Languages: "Language", Home: "Home",

		Articles: "Articles", Pages: "Pages", AllPages: "Every page of the issue", ReadPages: "Pages in the issue",
		NoArticles: "This issue is not split into articles yet.", NoPages: "No page of this issue has been read yet.", Empty: "Nothing here yet.",
		Unnumbered: "unnumbered",
		Prev:       "Previous", Next: "Next", PrevIssue: "Previous issue", NextIssue: "Next issue",
		PrevYear: "Previous year", NextYear: "Next year",
		Issue: "Issue", Year: "Year",

		Original:    "Original",
		Translation: "Machine translation from the Russian",
		Transcribed: "Transcribed from the scan",
		NativeText:  "Taken from the PDF's text layer",
		NotChecked:  "Not checked by a person.",
		Draft:       "Rough automatic draft, low quality: expect mistakes and awkward wording. It will be replaced by a better translation.",
		DraftShort:  "Rough draft translation, low quality",
		Tag:         "Tag",
		TagLede:     "A permanent address. It stays with the piece for good, even when the piece is renamed or moved.",
		TagIndexLede: "Every piece carries a four character tag. " +
			"It is permanent and does not change when the piece is renamed or moved.",
		TagLookup:   "Enter a tag",
		RubricsLede: "The standing sections of the magazine.",
		AuthorsLede: "Names as they are printed over the articles.",
		Math:        "Mathematics", Physics: "Physics", WithSolution: "solution printed", InOriginal: "opens in the original Russian",
		ProblemsLede: "The M and F problems under their own numbers. The statement and the solution ran in different issues, so a problem stands apart from both.",
		SignedAlso:   "Also signed",
		Posed:        "Statement", Solved: "Solution", NoSolution: "solution not printed",
		ChooseLanguage: "Choose a language",
		NotFound:       "Page not found",
		NotFoundLede:   "There is no such page. The piece may not have been read yet.",
		Source:         "Source",

		issue: "Kvant %d, No. %s", year: "Kvant %d", sheet: "Sheet %d", page: "p. %s", problem: "Problem %s", tagOn: "Tags starting with %s",
		count: func(n int, what string) string {
			forms := map[string][2]string{
				"article": {"piece", "pieces"},
				"issue":   {"issue", "issues"},
				"page":    {"page", "pages"},
				"tag":     {"tag", "tags"},
				"problem": {"problem", "problems"},
				"author":  {"author", "authors"},
			}[what]
			if n == 1 {
				return fmt.Sprintf("%d %s", n, forms[0])
			}
			return fmt.Sprintf("%d %s", n, forms[1])
		},
	},
	"vi": {
		Name:    "Tiếng Việt",
		Brand:   "Kvant",
		Tagline: "Tạp chí phổ biến khoa học vật lý và toán học",
		About:   "Mọi trang của tạp chí từ năm 1970: các bài báo, mục bài toán, các kỳ olympic và mọi thứ được in xen giữa. Được sắp chữ lại, công thức được dàn trang, mỗi bài có một địa chỉ cố định.",

		Archive: "Lưu trữ", Rubrics: "Chuyên mục", Authors: "Tác giả", Problems: "Bài toán", Tags: "Thẻ", Search: "Tìm kiếm",
		SearchHint:  "Tiêu đề, tác giả, chuyên mục hoặc thẻ",
		SearchNoJS:  "Tìm kiếm cần JavaScript. Không có nó, toàn bộ lưu trữ vẫn truy cập được qua chuyên mục, tác giả và các số báo.",
		SearchEmpty: "Hãy nhập từ khoá.",
		SearchNone:  "Không tìm thấy gì.",
		Menu:        "Menu", Theme: "Sáng hoặc tối", Contents: "Mục lục", OnThisPage: "Trên trang này", BackToTop: "Lên đầu trang",
		Skip: "Đến nội dung", Languages: "Ngôn ngữ", Home: "Trang chủ",

		Articles: "Bài báo", Pages: "Trang", AllPages: "Mọi trang của số báo", ReadPages: "Các trang trong số",
		NoArticles: "Số này chưa được tách thành bài.", NoPages: "Chưa trang nào của số này được đọc.", Empty: "Chưa có gì.",
		Unnumbered: "không đánh số",
		Prev:       "Trước", Next: "Tiếp", PrevIssue: "Số trước", NextIssue: "Số sau",
		PrevYear: "Năm trước", NextYear: "Năm sau",
		Issue: "Số", Year: "Năm",

		Original:    "Bản gốc",
		Translation: "Bản dịch máy từ tiếng Nga",
		Transcribed: "Chép lại từ bản quét",
		NativeText:  "Lấy từ lớp văn bản của PDF",
		NotChecked:  "Chưa được người kiểm tra.",
		Draft:       "Bản dịch tự động sơ bộ, chất lượng thấp: có thể còn lỗi và câu chữ chưa tự nhiên. Bản này sẽ được thay bằng bản dịch tốt hơn.",
		DraftShort:  "Bản dịch sơ bộ, chất lượng thấp",
		Tag:         "Thẻ",
		TagLede:     "Một địa chỉ cố định. Nó gắn với bài mãi mãi, kể cả khi bài đổi tên hay đổi chỗ.",
		TagIndexLede: "Mỗi bài có một thẻ bốn ký tự. " +
			"Thẻ là cố định và không đổi khi bài được đổi tên hay chuyển chỗ.",
		TagLookup:   "Nhập thẻ",
		RubricsLede: "Các chuyên mục thường kỳ của tạp chí.",
		AuthorsLede: "Tên như được in trên các bài báo.",
		Math:        "Toán học", Physics: "Vật lý", WithSolution: "đã in lời giải", InOriginal: "mở bản gốc tiếng Nga",
		ProblemsLede: "Các bài toán M và F theo số của chúng. Đề bài và lời giải in ở các số khác nhau, nên bài toán đứng riêng.",
		SignedAlso:   "Cũng ký là",
		Posed:        "Đề bài", Solved: "Lời giải", NoSolution: "chưa in lời giải",
		ChooseLanguage: "Chọn ngôn ngữ",
		NotFound:       "Không tìm thấy trang",
		NotFoundLede:   "Không có trang này. Có thể bài chưa được đọc.",
		Source:         "Mã nguồn",

		issue: "Kvant %d, số %s", year: "Kvant %d", sheet: "Tờ %d", page: "tr. %s", problem: "Bài %s", tagOn: "Thẻ bắt đầu bằng %s",
		count: func(n int, what string) string {
			noun := map[string]string{
				"article": "bài", "issue": "số", "page": "trang", "tag": "thẻ", "problem": "bài toán", "author": "tác giả",
			}[what]
			return fmt.Sprintf("%d %s", n, noun)
		},
	},
	"zh": {
		Name:    "中文",
		Brand:   "Kvant",
		Tagline: "物理与数学科普杂志",
		About:   "杂志自1970年以来的每一页：文章、习题栏、奥林匹克竞赛，以及其间刊登的一切。重新排版，公式重新排印，每篇文章都有固定的地址。",

		Archive: "档案", Rubrics: "栏目", Authors: "作者", Problems: "习题", Tags: "标签", Search: "搜索",
		SearchHint:  "标题、作者、栏目或标签",
		SearchNoJS:  "搜索需要 JavaScript。没有它，整个档案仍可通过栏目、作者和各期访问。",
		SearchEmpty: "请输入关键词。",
		SearchNone:  "没有找到结果。",
		Menu:        "菜单", Theme: "浅色或深色", Contents: "目录", OnThisPage: "本页内容", BackToTop: "回到顶部",
		Skip: "跳到正文", Languages: "语言", Home: "首页",

		Articles: "文章", Pages: "页面", AllPages: "本期所有页面", ReadPages: "本期页面",
		NoArticles: "本期尚未拆分为文章。", NoPages: "本期尚无已识别的页面。", Empty: "暂无内容。",
		Unnumbered: "无页码",
		Prev:       "上一篇", Next: "下一篇", PrevIssue: "上一期", NextIssue: "下一期",
		PrevYear: "上一年", NextYear: "下一年",
		Issue: "期", Year: "年",

		Original:    "原文",
		Translation: "由俄语机器翻译",
		Transcribed: "据扫描件录入",
		NativeText:  "取自 PDF 的文字层",
		NotChecked:  "尚未经人工校对。",
		Draft:       "自动生成的粗略译稿，质量较低：可能有错误，语句也不够通顺。之后会被更好的译文取代。",
		DraftShort:  "粗略译稿，质量较低",
		Tag:         "标签",
		TagLede:     "一个固定的地址。即使文章改名或移动，它也永远指向这篇文章。",
		TagIndexLede: "每篇文章都有一个四个字符的标签。" +
			"标签是固定的，文章改名或移动时也不会改变。",
		TagLookup:   "输入标签",
		RubricsLede: "杂志的常设栏目。",
		AuthorsLede: "姓名按文章上刊印的写法。",
		Math:        "数学", Physics: "物理", WithSolution: "已刊登解答", InOriginal: "打开俄文原文",
		ProblemsLede: "按编号排列的 M 题和 F 题。题目和解答刊登在不同的期号中，因此每道题单独成页。",
		SignedAlso:   "另署名为",
		Posed:        "题目", Solved: "解答", NoSolution: "尚未刊登解答",
		ChooseLanguage: "选择语言",
		NotFound:       "页面未找到",
		NotFoundLede:   "没有这个页面。也许这篇文章还没有被识别。",
		Source:         "源代码",

		issue: "Kvant %d 年第 %s 期", year: "Kvant %d 年", sheet: "第 %d 张", page: "第 %s 页", problem: "习题 %s", tagOn: "以 %s 开头的标签",
		count: func(n int, what string) string {
			noun := map[string]string{
				"article": "篇文章", "issue": "期", "page": "页", "tag": "个标签", "problem": "道习题", "author": "位作者",
			}[what]
			return fmt.Sprintf("%d %s", n, noun)
		},
	},
}

// wordsFor is the frame in a language, or in English when the table does not
// have that one.
func wordsFor(lang string) words {
	if w, ok := languages[lang]; ok {
		return w
	}
	w := languages["en"]
	w.Name = strings.ToUpper(lang)
	return w
}

// rubricNames is the standing sections in the languages other than the one
// the magazine printed them in. The slug is the key, as it is everywhere.
var rubricNames = map[string]map[string]string{
	"en": {
		"zadachnik-kvanta":                  "Kvant Problem Book",
		"kvant-dlya-mladshih-shkolnikov":    "Kvant for Younger Students",
		"praktikum-abiturienta":             "University Applicant's Workshop",
		"matematicheskiy-kruzhok":           "Mathematics Circle",
		"fizicheskiy-fakultativ":            "Physics Elective",
		"laboratoriya-kvanta":               "Kvant Laboratory",
		"kaleydoskop-kvanta":                "Kvant Kaleidoscope",
		"nashi-nablyudeniya":                "Our Observations",
		"shahmatnaya-stranichka":            "Chess Page",
		"otvety-ukazaniya-resheniya":        "Answers, Hints, Solutions",
		"novosti-nauki":                     "Science News",
		"informatsiya":                      "Information",
		"retsenzii-bibliografiya":           "Reviews and Bibliography",
		"zadachi-nashih-chitateley":         "Problems from Our Readers",
		"iz-istorii-nauki":                  "From the History of Science",
		"smes":                              "Miscellany",
		"shkola-v-kvante":                   "School in Kvant",
		"olimpiady":                         "Olympiads",
		"ekzamenatsionnye-materialy":        "Examination Materials",
		"igry-i-golovolomki":                "Games and Puzzles",
		"konkurs-imeni-a-p-savina":          "A. P. Savin Competition",
		"kvant-ulybaetsya":                  "Kvant Smiles",
		"nam-pishut":                        "Letters to the Editor",
		"o-lyudyah":                         "About People",
		"matematicheskiy-mir":               "Mathematical World",
		"ugolok-kollektsionera":             "Collector's Corner",
		"po-stranitsam-shkolnyh-uchebnikov": "Through the Pages of School Textbooks",
		"osnovnye-stati":                    "Main Articles",
		"raznoe":                            "Other",
		"otkryvayushchie-stati":             "Opening Articles",
	},
	"vi": {
		"zadachnik-kvanta":                  "Bài toán Kvant",
		"kvant-dlya-mladshih-shkolnikov":    "Kvant cho học sinh nhỏ",
		"praktikum-abiturienta":             "Luyện thi đại học",
		"matematicheskiy-kruzhok":           "Câu lạc bộ toán học",
		"fizicheskiy-fakultativ":            "Vật lý tự chọn",
		"laboratoriya-kvanta":               "Phòng thí nghiệm Kvant",
		"kaleydoskop-kvanta":                "Kính vạn hoa Kvant",
		"nashi-nablyudeniya":                "Quan sát của chúng tôi",
		"shahmatnaya-stranichka":            "Trang cờ vua",
		"otvety-ukazaniya-resheniya":        "Đáp số, gợi ý, lời giải",
		"novosti-nauki":                     "Tin khoa học",
		"informatsiya":                      "Thông tin",
		"retsenzii-bibliografiya":           "Điểm sách và thư mục",
		"zadachi-nashih-chitateley":         "Bài toán của bạn đọc",
		"iz-istorii-nauki":                  "Từ lịch sử khoa học",
		"smes":                              "Tạp lục",
		"shkola-v-kvante":                   "Trường học trong Kvant",
		"olimpiady":                         "Olympic",
		"ekzamenatsionnye-materialy":        "Tài liệu thi cử",
		"igry-i-golovolomki":                "Trò chơi và câu đố",
		"konkurs-imeni-a-p-savina":          "Cuộc thi mang tên A. P. Savin",
		"kvant-ulybaetsya":                  "Kvant mỉm cười",
		"nam-pishut":                        "Thư bạn đọc",
		"o-lyudyah":                         "Về con người",
		"matematicheskiy-mir":               "Thế giới toán học",
		"ugolok-kollektsionera":             "Góc sưu tầm",
		"po-stranitsam-shkolnyh-uchebnikov": "Qua các trang sách giáo khoa",
		"osnovnye-stati":                    "Bài chính",
		"raznoe":                            "Khác",
		"otkryvayushchie-stati":             "Bài mở đầu",
	},
	"zh": {
		"zadachnik-kvanta":                  "Kvant 习题集",
		"kvant-dlya-mladshih-shkolnikov":    "Kvant 低年级专栏",
		"praktikum-abiturienta":             "升学备考",
		"matematicheskiy-kruzhok":           "数学小组",
		"fizicheskiy-fakultativ":            "物理选修",
		"laboratoriya-kvanta":               "Kvant 实验室",
		"kaleydoskop-kvanta":                "Kvant 万花筒",
		"nashi-nablyudeniya":                "我们的观察",
		"shahmatnaya-stranichka":            "国际象棋",
		"otvety-ukazaniya-resheniya":        "答案、提示与解答",
		"novosti-nauki":                     "科学新闻",
		"informatsiya":                      "信息",
		"retsenzii-bibliografiya":           "书评与书目",
		"zadachi-nashih-chitateley":         "读者来题",
		"iz-istorii-nauki":                  "科学史话",
		"smes":                              "杂录",
		"shkola-v-kvante":                   "Kvant 学校",
		"olimpiady":                         "奥林匹克",
		"ekzamenatsionnye-materialy":        "考试资料",
		"igry-i-golovolomki":                "游戏与谜题",
		"konkurs-imeni-a-p-savina":          "A. P. 萨温竞赛",
		"kvant-ulybaetsya":                  "Kvant 微笑",
		"nam-pishut":                        "读者来信",
		"o-lyudyah":                         "人物",
		"matematicheskiy-mir":               "数学世界",
		"ugolok-kollektsionera":             "收藏角",
		"po-stranitsam-shkolnyh-uchebnikov": "课本拾遗",
		"osnovnye-stati":                    "主要文章",
		"raznoe":                            "其他",
		"otkryvayushchie-stati":             "开篇文章",
	},
}

// rubricTitle is a section's name in a language, or the name the magazine
// printed when there is no translation of it.
func rubricTitle(lang, slug string) string {
	if name, ok := rubricNames[lang][slug]; ok {
		return name
	}
	if r := rubric.BySlug(slug); r.Title != "" {
		return r.Title
	}
	return slug
}
