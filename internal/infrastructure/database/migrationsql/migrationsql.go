// Package migrationsql rejects migration SQL that would break the atomicity of
// applying it.
//
// goose runs a migration and writes its history row in one transaction, and
// that single commit is the entire reason an interrupted migration leaves
// nothing behind. Two things in a migration file take it away: the NO
// TRANSACTION annotation, which makes goose run the file outside a transaction
// and record the history row separately, and transaction-control statements in
// the SQL itself, which end goose's transaction from the inside without any
// annotation being involved. Either way an interruption can leave committed
// DDL that no history row accounts for, and the next run will try to apply it
// again.
//
// Scanning is a source-registration check: it reads text, touches no database,
// and is therefore the last point at which such a file can still be refused
// cheaply.
package migrationsql

import (
	"fmt"
	"sort"
	"strings"
)

// Violation is one reason a migration file is not accepted.
type Violation struct {
	// Line is the 1-based line the offending annotation or statement starts on.
	Line int
	// Reason names what was found and what the author should do instead.
	Reason string
}

// Scan reports every reason the given migration SQL cannot be accepted, in
// source order. A nil result means the file is acceptable.
func Scan(sql string) []Violation {
	violations := append(scanAnnotations(sql), scanStatements(sql)...)
	sort.SliceStable(violations, func(i, j int) bool { return violations[i].Line < violations[j].Line })
	return violations
}

const (
	noTransactionReason = "-- +goose NO TRANSACTION applies this migration outside a transaction and writes its " +
		"history row separately, so an interruption can leave the schema changed with nothing recording it; keep the " +
		"migration transactional and perform anything that genuinely cannot be (CREATE INDEX CONCURRENTLY, " +
		"ALTER TYPE ... ADD VALUE) as an operational step outside the migration history"

	envsubOnReason = "-- +goose ENVSUB ON substitutes environment variables into the SQL before it runs, so the " +
		"statements checked here are not the statements that reach the database; write the values into the migration"
)

// bannedAnnotations are matched against a comment line reduced to lowercase,
// single-spaced text — the same normalization goose applies before comparing
// annotations case-insensitively.
var bannedAnnotations = map[string]string{
	"+goose no transaction": noTransactionReason,
	"+goose envsub on":      envsubOnReason,
}

// scanAnnotations looks at raw lines rather than at parsed SQL because that is
// how goose reads annotations: every line of the file is tested, so a
// directive sitting inside a function body or a StatementBegin block still
// takes effect and still has to be caught here.
func scanAnnotations(sql string) []Violation {
	var violations []Violation
	for i, line := range strings.Split(sql, "\n") {
		text, isComment := strings.CutPrefix(strings.TrimSpace(line), "--")
		if !isComment {
			continue
		}
		normalized := strings.ToLower(strings.Join(strings.Fields(text), " "))
		if reason, banned := bannedAnnotations[normalized]; banned {
			violations = append(violations, Violation{Line: i + 1, Reason: reason})
		}
	}
	return violations
}

// transactionControlVerbs are the statement verbs that begin, end or reposition
// a transaction. PREPARE is not among them: it is only transaction control when
// followed by TRANSACTION, which [verdict] checks separately.
var transactionControlVerbs = map[string]struct{}{
	"begin":     {},
	"start":     {},
	"commit":    {},
	"end":       {},
	"rollback":  {},
	"abort":     {},
	"savepoint": {},
	"release":   {},
}

// trackedWords is how much of a statement decides the verdict: its verb, and
// for PREPARE the word that separates two-phase commit from an ordinary
// PREPARE ... AS.
const trackedWords = 2

// scanStatements splits the SQL into top-level statements and judges each one
// by the keyword it starts with.
//
// goose's StatementBegin and StatementEnd annotations deliberately play no part
// here. They only tell goose to stop splitting on semicolons; the text between
// them is still executed, so a bare COMMIT wrapped in them ends the transaction
// exactly as an unwrapped one would. What is genuinely exempt is a
// dollar-quoted string: that is a function or procedure body, not a sequence of
// statements the server will run now.
func scanStatements(sql string) []Violation {
	s := &scanner{src: sql, line: 1}
	s.run()
	return s.violations
}

type scanner struct {
	src        string
	pos        int
	line       int
	violations []Violation

	// stmtLine is 0 until the current statement has produced a token, which is
	// also how an empty statement (a stray semicolon) is recognized.
	stmtLine  int
	stmtWords []string
}

func (s *scanner) run() {
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch {
		case c == '\n':
			s.line++
			s.pos++
		case c == '-' && s.nextIs('-'):
			s.skipLineComment()
		case c == '/' && s.nextIs('*'):
			s.skipBlockComment()
		case c == '\'' || c == '"':
			s.note("")
			s.skipQuoted(c, false)
		case c == '$':
			s.skipDollarQuoted()
		case c == ';':
			s.endStatement()
			s.pos++
		case isWordByte(c):
			s.word()
		case isSpaceByte(c):
			s.pos++
		default:
			s.note("")
			s.pos++
		}
	}
	s.endStatement()
}

func (s *scanner) word() {
	start := s.pos
	for s.pos < len(s.src) && isWordByte(s.src[s.pos]) {
		s.pos++
	}
	word := s.src[start:s.pos]
	s.note(word)

	// E'...' honors backslash escapes, so E'\'' is one string where '\'' would
	// be two. Reading it here keeps a mis-paired quote from swallowing the
	// semicolons that separate the statements after it.
	if strings.EqualFold(word, "e") && s.pos < len(s.src) && s.src[s.pos] == '\'' {
		s.skipQuoted('\'', true)
	}
}

// note records one significant token of the current statement. Tokens that are
// not bare words — literals, operators, punctuation — are recorded as an empty
// word: it never matches a keyword, but it still occupies a slot, so
// "PREPARE 'plan' AS ..." is not mistaken for "PREPARE TRANSACTION".
func (s *scanner) note(word string) {
	if s.stmtLine == 0 {
		s.stmtLine = s.line
	}
	if len(s.stmtWords) < trackedWords {
		s.stmtWords = append(s.stmtWords, strings.ToLower(word))
	}
}

func (s *scanner) endStatement() {
	line, words := s.stmtLine, s.stmtWords
	s.stmtLine, s.stmtWords = 0, nil

	if keyword, banned := verdict(words); banned {
		s.violations = append(s.violations, Violation{Line: line, Reason: statementReason(keyword)})
	}
}

// verdict names the transaction-control construct a statement starts with.
//
// END is the ambiguous one: it also closes a CASE expression and a PL/pgSQL
// block. Both of those can only appear inside a larger statement or inside a
// dollar-quoted body, and neither is judged here — so a top-level statement
// whose own first word is END is ending a transaction.
func verdict(words []string) (string, bool) {
	if len(words) == 0 {
		return "", false
	}
	if words[0] == "prepare" {
		if len(words) > 1 && words[1] == "transaction" {
			return "PREPARE TRANSACTION", true
		}
		return "", false
	}
	if _, banned := transactionControlVerbs[words[0]]; banned {
		return strings.ToUpper(words[0]), true
	}
	return "", false
}

func statementReason(keyword string) string {
	return fmt.Sprintf(
		"%s controls the transaction goose opened for this migration; a migration must not manage its own "+
			"transaction, because the schema change and the history row that records it have to commit together — "+
			"remove the statement and let goose commit the file as a whole",
		keyword,
	)
}

func (s *scanner) nextIs(c byte) bool {
	return s.pos+1 < len(s.src) && s.src[s.pos+1] == c
}

// advance moves the cursor to end, counting the lines crossed on the way.
func (s *scanner) advance(end int) {
	s.line += strings.Count(s.src[s.pos:end], "\n")
	s.pos = end
}

// skipLineComment stops at the newline rather than consuming it, so the main
// loop stays the only place lines are counted for it.
func (s *scanner) skipLineComment() {
	if i := strings.IndexByte(s.src[s.pos:], '\n'); i >= 0 {
		s.pos += i
		return
	}
	s.pos = len(s.src)
}

// skipBlockComment honors nesting, which PostgreSQL supports: the outer comment
// in "/* a /* b */ c */" ends at the last delimiter, not the first.
func (s *scanner) skipBlockComment() {
	depth := 0
	for i := s.pos; i+1 < len(s.src); {
		switch {
		case s.src[i] == '/' && s.src[i+1] == '*':
			depth++
			i += 2
		case s.src[i] == '*' && s.src[i+1] == '/':
			depth--
			i += 2
			if depth == 0 {
				s.advance(i)
				return
			}
		default:
			i++
		}
	}
	s.advance(len(s.src))
}

// skipQuoted consumes a '...' literal or a "..." identifier. A doubled quote
// escapes itself in both; backslash escapes apply only to E'...' strings.
func (s *scanner) skipQuoted(quote byte, backslashEscapes bool) {
	for i := s.pos + 1; i < len(s.src); i++ {
		if backslashEscapes && s.src[i] == '\\' {
			i++
			continue
		}
		if s.src[i] != quote {
			continue
		}
		if i+1 < len(s.src) && s.src[i+1] == quote {
			i++
			continue
		}
		s.advance(i + 1)
		return
	}
	s.advance(len(s.src))
}

// skipDollarQuoted consumes a $$...$$ or $tag$...$tag$ string whole. Nothing
// inside one is examined: it is a function or procedure body, so a COMMIT there
// is text handed to the server for later, not a statement running now. A
// semicolon inside one does not end a statement either.
//
// A dollar sign that opens no such string — a positional parameter like $1, or
// a lone $ — is just another opaque token.
func (s *scanner) skipDollarQuoted() {
	tag, opens := s.dollarTag()
	if !opens {
		s.note("")
		s.pos++
		return
	}

	s.note("")
	s.pos += len(tag)
	if i := strings.Index(s.src[s.pos:], tag); i >= 0 {
		s.advance(s.pos + i + len(tag))
		return
	}
	s.advance(len(s.src))
}

// dollarTag returns the opening delimiter, "$$" or "$tag$", at the cursor. The
// tag follows the rule for an unquoted identifier: letters, digits and
// underscores, not starting with a digit.
func (s *scanner) dollarTag() (string, bool) {
	end := s.pos + 1
	for end < len(s.src) && isWordByte(s.src[end]) {
		end++
	}
	if end >= len(s.src) || s.src[end] != '$' {
		return "", false
	}
	if end > s.pos+1 && isDigitByte(s.src[s.pos+1]) {
		return "", false
	}
	return s.src[s.pos : end+1], true
}

func isWordByte(c byte) bool {
	return c == '_' || isDigitByte(c) || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isDigitByte(c byte) bool { return c >= '0' && c <= '9' }

func isSpaceByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v'
}
