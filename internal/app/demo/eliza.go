package demo

import (
	_ "embed"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

//go:embed doctor.script
var doctorScript string

const (
	noneKey        = "NONE"
	memoryKey      = "MEMORY"
	newKeyWord     = "NEWKEY"
	rewriteWord    = "PRE"
	dictionaryWord = "DLIST"
	redirectMark   = "="
	choiceMark     = "*"
	tagMark        = "/"
)

const (
	redirectLimit  = 16
	memoryBits     = 2
	countingCycle  = 4
	recallingCount = 4
)

var confusions = []string{"PLEASE CONTINUE", "HMMM", "GO ON , PLEASE", "I SEE"}

type partKind int

const (
	literalPart partKind = iota
	anyPart
	exactPart
	choicePart
	tagPart
)

type patternPart struct {
	kind    partKind
	literal string
	count   int
	words   []string
}

type reassembly struct {
	words      []string
	redirect   string
	rewrite    []string
	rewriteKey string
	isNewKey   bool
}

type transform struct {
	pattern      []patternPart
	reassemblies []reassembly
}

type rule struct {
	key        string
	substitute string
	redirect   string
	rank       int
	tags       []string
	transforms []transform
}

type script struct {
	greeting      string
	memoryTrigger string
	lastResort    *rule
	rules         map[string]*rule
	memories      []transform
}

type node struct {
	atom     string
	children []*node
	isList   bool
}

var delimiterWords = []string{".", ",", "BUT"}

var pronounSpellings = map[string]string{
	"i":    "I",
	"i'm":  "I'm",
	"i've": "I've",
	"i'd":  "I'd",
	"i'll": "I'll",
}

var loadScript = sync.OnceValues(func() (*script, error) {
	return parseScript(doctorScript)
})

func parseScript(source string) (*script, error) {
	nodes, err := parseNodes(scriptTokens(source))
	if err != nil {
		return nil, err
	}

	self := &script{rules: map[string]*rule{}}

	for _, top := range nodes {
		if !top.isList || len(top.children) == 0 {
			continue
		}

		if self.greeting == "" {
			self.greeting = joinAtoms(top.children)
			continue
		}

		if top.children[0].atom == memoryKey {
			trigger, memories, err := buildMemories(top)
			if err != nil {
				return nil, err
			}

			self.memoryTrigger = trigger
			self.memories = memories

			continue
		}

		newRule, err := buildRule(top)
		if err != nil {
			return nil, err
		}

		if newRule.key == noneKey {
			self.lastResort = newRule
			continue
		}

		self.rules[newRule.key] = newRule
	}

	return self, nil
}

func scriptTokens(source string) []string {
	var tokens []string

	var current strings.Builder

	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}

	isComment := false

	for _, letter := range source {
		switch {
		case isComment:
			if letter == '\n' {
				isComment = false
			}
		case letter == ';':
			flush()

			isComment = true
		case letter == '(' || letter == ')':
			flush()
			tokens = append(tokens, string(letter))
		case unicode.IsSpace(letter):
			flush()
		default:
			current.WriteRune(letter)
		}
	}

	flush()

	return tokens
}

func parseNodes(tokens []string) ([]*node, error) {
	var nodes []*node

	at := 0

	for at < len(tokens) {
		parsedNode, next, err := parseNode(tokens, at)
		if err != nil {
			return nil, err
		}

		nodes = append(nodes, parsedNode)
		at = next
	}

	return nodes, nil
}

func parseNode(tokens []string, at int) (*node, int, error) {
	if tokens[at] != "(" {
		if tokens[at] == ")" {
			return nil, 0, fmt.Errorf("the script closes a list that was never opened at token %d", at)
		}

		return &node{atom: tokens[at]}, at + 1, nil
	}

	list := &node{isList: true}

	for at++; at < len(tokens); {
		if tokens[at] == ")" {
			return list, at + 1, nil
		}

		child, next, err := parseNode(tokens, at)
		if err != nil {
			return nil, 0, err
		}

		list.children = append(list.children, child)
		at = next
	}

	return nil, 0, fmt.Errorf("the script leaves a list open at token %d", at)
}

func buildRule(list *node) (*rule, error) {
	self := &rule{key: list.children[0].atom}
	at := 1

	if at < len(list.children) && list.children[at].atom == redirectMark {
		if at+1 >= len(list.children) {
			return nil, fmt.Errorf("%s substitutes nothing", self.key)
		}

		self.substitute = list.children[at+1].atom
		at += 2
	}

	if at < len(list.children) && !list.children[at].isList {
		rank, err := strconv.Atoi(list.children[at].atom)
		if err == nil {
			self.rank = rank
			at++
		}
	}

	if at < len(list.children) && list.children[at].atom == dictionaryWord {
		if at+1 >= len(list.children) || !list.children[at+1].isList {
			return nil, fmt.Errorf("%s names no tags", self.key)
		}

		self.tags = markedWords(list.children[at+1], tagMark)
		at += 2
	}

	for ; at < len(list.children); at++ {
		child := list.children[at]
		if !child.isList || len(child.children) == 0 {
			return nil, fmt.Errorf("%s carries something that is not a rule", self.key)
		}

		if key, isRedirect := redirectedKey(child); isRedirect {
			self.redirect = key
			continue
		}

		newTransform, err := buildTransform(child)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", self.key, err)
		}

		self.transforms = append(self.transforms, newTransform)
	}

	return self, nil
}

func buildTransform(list *node) (transform, error) {
	if !list.children[0].isList {
		return transform{}, fmt.Errorf("a rule starts with %q rather than a decomposition", list.children[0].atom)
	}

	self := transform{pattern: buildPattern(list.children[0])}

	for _, child := range list.children[1:] {
		if !child.isList || len(child.children) == 0 {
			return transform{}, errors.New("a decomposition carries something that is not a reassembly")
		}

		self.reassemblies = append(self.reassemblies, buildReassembly(child))
	}

	return self, nil
}

func buildPattern(list *node) []patternPart {
	parts := make([]patternPart, 0, len(list.children))

	for _, child := range list.children {
		switch {
		case child.isList && len(child.children) > 0 && strings.HasPrefix(child.children[0].atom, choiceMark):
			parts = append(parts, patternPart{kind: choicePart, words: markedWords(child, choiceMark)})
		case child.isList:
			parts = append(parts, patternPart{kind: tagPart, words: markedWords(child, tagMark)})
		default:
			count, err := strconv.Atoi(child.atom)
			switch {
			case err != nil:
				parts = append(parts, patternPart{kind: literalPart, literal: child.atom})
			case count == 0:
				parts = append(parts, patternPart{kind: anyPart})
			default:
				parts = append(parts, patternPart{kind: exactPart, count: count})
			}
		}
	}

	return parts
}

func buildReassembly(list *node) reassembly {
	if key, isRedirect := redirectedKey(list); isRedirect {
		return reassembly{redirect: key}
	}

	if list.children[0].atom == newKeyWord {
		return reassembly{isNewKey: true}
	}

	if list.children[0].atom == rewriteWord && len(list.children) == 3 {
		key, isRedirect := redirectedKey(list.children[2])
		if isRedirect {
			return reassembly{rewrite: atoms(list.children[1]), rewriteKey: key}
		}
	}

	return reassembly{words: atoms(list)}
}

func buildMemories(list *node) (string, []transform, error) {
	if len(list.children) < 2 || list.children[1].isList {
		return "", nil, errors.New("the memory names no keyword")
	}

	var memories []transform

	for _, child := range list.children[2:] {
		if !child.isList {
			return "", nil, fmt.Errorf("the memory carries the atom %q", child.atom)
		}

		divide := slices.IndexFunc(child.children, func(part *node) bool {
			return !part.isList && part.atom == redirectMark
		})
		if divide < 0 {
			return "", nil, errors.New("a memory has no reassembly")
		}

		memories = append(memories, transform{
			pattern:      buildPattern(&node{isList: true, children: child.children[:divide]}),
			reassemblies: []reassembly{{words: atoms(&node{isList: true, children: child.children[divide+1:]})}},
		})
	}

	return list.children[1].atom, memories, nil
}

func redirectedKey(list *node) (string, bool) {
	if !list.isList || len(list.children) == 0 || list.children[0].isList {
		return "", false
	}

	head := list.children[0].atom

	if head == redirectMark && len(list.children) == 2 {
		return list.children[1].atom, true
	}

	if len(list.children) == 1 && strings.HasPrefix(head, redirectMark) && len(head) > 1 {
		return head[1:], true
	}

	return "", false
}

func markedWords(list *node, mark string) []string {
	words := atoms(list)
	if len(words) == 0 {
		return nil
	}

	words[0] = strings.TrimPrefix(words[0], mark)
	if words[0] == "" {
		words = words[1:]
	}

	return words
}

func atoms(list *node) []string {
	words := make([]string, 0, len(list.children))

	for _, child := range list.children {
		if !child.isList {
			words = append(words, child.atom)
		}
	}

	return words
}

func joinAtoms(nodes []*node) string {
	return strings.Join(atoms(&node{isList: true, children: nodes}), " ")
}

type outcome int

const (
	completed outcome = iota
	inapplicable
	linkedKey
	nextKey
)

type doctor struct {
	script   *script
	cycles   map[string]int
	memories []string
	count    int
}

func consultDoctor() (*doctor, error) {
	loadedScript, err := loadScript()
	if err != nil {
		return nil, err
	}

	return &doctor{script: loadedScript, cycles: map[string]int{}, count: 1}, nil
}

func (self *doctor) Reply(sentence string) string {
	self.count = self.count%countingCycle + 1

	words, keystack := self.script.scan(tokenise(sentence))

	if len(keystack) == 0 {
		if self.count == recallingCount && len(self.memories) > 0 {
			memory := self.memories[0]
			self.memories = self.memories[1:]

			return memory
		}

		return self.none(words)
	}

	for range redirectLimit {
		if len(keystack) == 0 {
			break
		}

		key := keystack[0]
		keystack = keystack[1:]

		found, isKnown := self.script.rules[key]
		if !isKnown {
			return self.confusion()
		}

		self.remember(key, words)

		reply, link, howItWent := self.transform(found, words)

		switch howItWent {
		case completed:
			return strings.Join(reply, " ")

		case inapplicable:
			return self.confusion()

		case linkedKey:
			words = reply
			keystack = append([]string{link}, keystack...)

		case nextKey:
			if len(keystack) == 0 {
				return self.none(words)
			}
		}
	}

	return self.none(words)
}

func (self *doctor) transform(found *rule, words []string) ([]string, string, outcome) {
	at, groups, hasMatch := self.script.match(found, words)
	if !hasMatch {
		if found.redirect == "" {
			return words, "", inapplicable
		}

		return words, found.redirect, linkedKey
	}

	choices := found.transforms[at].reassemblies
	cycle := fmt.Sprintf("%s:%d", found.key, at)
	choice := choices[self.cycles[cycle]%len(choices)]
	self.cycles[cycle]++

	switch {
	case choice.isNewKey:
		return words, "", nextKey

	case choice.redirect != "":
		return words, choice.redirect, linkedKey

	case choice.rewriteKey != "":
		return assemble(choice.rewrite, groups), choice.rewriteKey, linkedKey
	}

	return assemble(choice.words, groups), "", completed
}

func (self *doctor) none(words []string) string {
	if self.script.lastResort == nil {
		return self.confusion()
	}

	reply, _, _ := self.transform(self.script.lastResort, words)

	return strings.Join(reply, " ")
}

func (self *doctor) confusion() string {
	return confusions[self.count-1]
}

func (self *doctor) remember(key string, words []string) {
	if key != self.script.memoryTrigger || len(self.script.memories) == 0 {
		return
	}
	if len(words) == 0 {
		return
	}

	entry := self.script.memories[hash(lastChunkAsBCD(words[len(words)-1]), memoryBits)]

	groups, hasMatch := self.script.matchPattern(entry.pattern, words)
	if !hasMatch {
		return
	}

	self.memories = append(self.memories, strings.Join(assemble(entry.reassemblies[0].words, groups), " "))
}

func stack(keystack []string, key string, rank int, topRank int) ([]string, int) {
	if rank > topRank {
		return append([]string{key}, keystack...), rank
	}

	return append(keystack, key), topRank
}

func (self *rule) spelling(word string) string {
	if self.substitute == "" {
		return word
	}

	return self.substitute
}

func (self *rule) hasTransformation() bool {
	return len(self.transforms) > 0 || self.redirect != ""
}

func (self *script) scan(tokens []string) ([]string, []string) {
	var words []string

	var keystack []string

	topRank := 0

	for _, token := range tokens {
		if isDelimiter(token) {
			if len(keystack) == 0 {
				words = nil
				continue
			}

			break
		}

		found, isKnown := self.rules[token]
		if !isKnown {
			words = append(words, token)
			continue
		}

		if found.hasTransformation() {
			keystack, topRank = stack(keystack, token, found.rank, topRank)
		}

		words = append(words, found.spelling(token))
	}

	return words, keystack
}

func (self *script) match(found *rule, words []string) (int, []string, bool) {
	for at, candidate := range found.transforms {
		if groups, hasMatch := self.matchPattern(candidate.pattern, words); hasMatch {
			return at, groups, true
		}
	}

	return 0, nil, false
}

func (self *script) matchPattern(pattern []patternPart, words []string) ([]string, bool) {
	if len(pattern) == 0 {
		if len(words) == 0 {
			return nil, true
		}

		return nil, false
	}

	part := pattern[0]

	switch part.kind {
	case anyPart:
		for take := range len(words) + 1 {
			if groups, hasMatch := self.matchPattern(pattern[1:], words[take:]); hasMatch {
				return append([]string{strings.Join(words[:take], " ")}, groups...), true
			}
		}

		return nil, false

	case exactPart:
		if len(words) < part.count {
			return nil, false
		}

		if groups, hasMatch := self.matchPattern(pattern[1:], words[part.count:]); hasMatch {
			return append([]string{strings.Join(words[:part.count], " ")}, groups...), true
		}

		return nil, false

	case literalPart:
		if len(words) == 0 || words[0] != part.literal {
			return nil, false
		}

	case choicePart:
		if len(words) == 0 || !slices.Contains(part.words, words[0]) {
			return nil, false
		}

	case tagPart:
		if len(words) == 0 || !self.isTagged(words[0], part.words) {
			return nil, false
		}
	}

	if groups, hasMatch := self.matchPattern(pattern[1:], words[1:]); hasMatch {
		return append([]string{words[0]}, groups...), true
	}

	return nil, false
}

func (self *script) isTagged(word string, tags []string) bool {
	found, isKnown := self.rules[word]
	if !isKnown {
		return false
	}

	for _, tag := range tags {
		if slices.Contains(found.tags, tag) {
			return true
		}
	}

	return false
}

func assemble(words []string, groups []string) []string {
	phrase := make([]string, 0, len(words))

	for _, word := range words {
		at, err := strconv.Atoi(word)
		if err != nil || at < 1 || at > len(groups) {
			phrase = append(phrase, word)
			continue
		}

		phrase = append(phrase, strings.Fields(groups[at-1])...)
	}

	return phrase
}

func isDelimiter(word string) bool {
	return slices.Contains(delimiterWords, word)
}

var reinterpretations = map[rune]string{
	'\u2018': " ",
	'\u2019': "'",
	'`':      " ",
	'"':      " ",
	'\u00ab': " ",
	'\u00bb': " ",
	'\u201a': " ",
	'\u201b': " ",
	'\u201c': " ",
	'\u201d': " ",
	'\u201e': " ",
	'\u201f': " ",
	'\u2039': " ",
	'\u203a': " ",
	'!':      ".",
	'?':      ".",
	'\u00a1': " ",
	'\u00bf': " ",
	':':      ",",
	';':      ",",
	'\u2013': ",",
	'\u2014': ",",
	'\u00df': "SS",
	'\ufb00': "FF",
	'\ufb01': "FI",
	'\ufb02': "FL",
	'\ufb03': "FFI",
	'\ufb04': "FFL",
	'\ufb05': "ST",
	'\ufb06': "ST",
}

func tokenise(sentence string) []string {
	var tokens []string

	var current strings.Builder

	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}

	for _, letter := range sentence {
		text := string(unicode.ToUpper(letter))
		if replacement, isReinterpreted := reinterpretations[letter]; isReinterpreted {
			text = replacement
		}

		for _, character := range text {
			switch {
			case unicode.IsSpace(character):
				flush()
			case character == '.' || character == ',':
				flush()
				tokens = append(tokens, string(character))
			default:
				current.WriteRune(character)
			}
		}
	}

	flush()

	return tokens
}

func speak(reply string) string {
	words := strings.Fields(strings.ToLower(reply))

	for at, word := range words {
		if pronoun, isPronoun := pronounSpellings[word]; isPronoun {
			words[at] = pronoun
		}
	}

	speech := strings.Join(words, " ")
	speech = strings.ReplaceAll(speech, " ,", ",")
	speech = strings.ReplaceAll(speech, " .", ".")

	var sentences []string

	for _, sentence := range splitSentences(speech) {
		sentences = append(sentences, capitalise(punctuate(sentence)))
	}

	return strings.Join(sentences, " ")
}

func splitSentences(speech string) []string {
	var sentences []string

	for speech != "" {
		at := strings.Index(speech, ". ")
		if at < 0 {
			sentences = append(sentences, speech)
			break
		}

		sentences = append(sentences, speech[:at+1])
		speech = strings.TrimSpace(speech[at+2:])
	}

	return sentences
}

var questionOpenings = []string{
	"am", "are", "aren't", "can", "can't", "could", "couldn't", "did", "didn't", "do", "does",
	"doesn't", "don't", "had", "has", "have", "how", "is", "isn't", "shall", "should", "was",
	"wasn't", "were", "weren't", "what", "when", "where", "which", "who", "whom", "whose", "why",
	"will", "won't", "would", "wouldn't",
}

var questionPrepositions = []string{"about", "at", "for", "from", "in", "of", "on", "to", "with"}

var interrogatives = []string{"how", "what", "when", "where", "which", "who", "whom", "whose", "why"}

var pronouns = []string{"he", "i", "it", "she", "they", "we", "you"}

var terminators = []string{".", "?", "!", ",", "-"}

func punctuate(sentence string) string {
	if sentence == "" {
		return sentence
	}

	for _, terminator := range terminators {
		if strings.HasSuffix(sentence, terminator) {
			return sentence
		}
	}

	if asksSomething(strings.Fields(sentence)) {
		return sentence + "?"
	}

	return sentence + "."
}

func asksSomething(words []string) bool {
	if len(words) == 0 {
		return false
	}

	openingWord := bareWord(words[0])

	if slices.Contains(questionOpenings, openingWord) {
		return true
	}

	if len(words) < 2 {
		return false
	}

	if slices.Contains(questionPrepositions, openingWord) &&
		slices.Contains(interrogatives, bareWord(words[1])) {
		return true
	}

	return slices.Contains(pronouns, bareWord(words[len(words)-1])) &&
		slices.Contains(questionOpenings, bareWord(words[len(words)-2]))
}

func bareWord(word string) string {
	return strings.ToLower(strings.Trim(word, ".,-"))
}

func capitalise(sentence string) string {
	for at, letter := range sentence {
		if unicode.IsLetter(letter) {
			return sentence[:at] + string(unicode.ToUpper(letter)) + sentence[at+len(string(letter)):]
		}
	}

	return sentence
}

var hollerithEncoding = func() [256]byte {
	const undefined = 0xFF

	characters := [64]byte{
		'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 0, '=', '\'', 0, 0, 0,
		'+', 'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 0, '.', ')', 0, 0, 0,
		'-', 'J', 'K', 'L', 'M', 'N', 'O', 'P', 'Q', 'R', 0, '$', '*', 0, 0, 0,
		' ', '/', 'S', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z', 0, ',', '(', 0, 0, 0,
	}

	var table [256]byte

	for at := range table {
		table[at] = undefined
	}

	for code, character := range characters {
		if character != 0 {
			table[character] = byte(code)
		}
	}

	return table
}()

func lastChunkAsBCD(word string) uint64 {
	const (
		cellWidth      = 6
		characterWidth = 6
		undefined      = 0xFF
		lowSixBits     = 0x3F
	)

	chunk := ""
	if word != "" {
		chunk = word[(len(word)-1)/cellWidth*cellWidth:]
	}

	datum := uint64(0)

	for at := range cellWidth {
		character := byte(' ')
		if at < len(chunk) {
			character = chunk[at]
		}

		code := hollerithEncoding[character]
		if code == undefined {
			code = character & lowSixBits
		}

		datum = datum<<characterWidth | uint64(code)
	}

	return datum
}

func hash(datum uint64, bits int) int {
	const magnitudeBits = 35

	datum &= 1<<magnitudeBits - 1
	datum *= datum
	datum >>= magnitudeBits - bits/2

	//nolint:gosec // bits is at most 15, so the result fits comfortably
	return int(datum & (1<<bits - 1))
}
