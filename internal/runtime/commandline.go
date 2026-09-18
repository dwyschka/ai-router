package runtime

import (
	"fmt"
	"strings"
)

// ParseCommandLine zerlegt eine eingegebene Kommandozeile wie
// `ollama launch claude` oder `mein-tool --flag "zwei wörter"` in Kommando und
// Argumente. Es wird ausschließlich getrennt und zitiert — keine Shell, keine
// Expansion von Variablen, Globs oder Backticks. Damit bleibt der Startkontrakt
// derselbe wie bei den eingebauten Runtimes.
func ParseCommandLine(line string) (command string, args []string, err error) {
	tokens, err := splitTokens(line)
	if err != nil {
		return "", nil, err
	}
	if len(tokens) == 0 {
		return "", nil, fmt.Errorf("leeres Kommando")
	}
	return tokens[0], tokens[1:], nil
}

// splitTokens trennt an Leerraum und respektiert einfache wie doppelte
// Anführungszeichen; ein Backslash schützt das nächste Zeichen.
func splitTokens(line string) ([]string, error) {
	var (
		tokens  []string
		current strings.Builder
		quote   rune
		escaped bool
		offen   bool // ein Token ist begonnen, auch wenn es leer bleibt ("")
	)

	for _, r := range line {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped = true
			offen = true
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			current.WriteRune(r)
		case r == '\'' || r == '"':
			quote = r
			offen = true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if current.Len() > 0 || offen {
				tokens = append(tokens, current.String())
				current.Reset()
				offen = false
			}
		default:
			current.WriteRune(r)
		}
	}

	if quote != 0 {
		return nil, fmt.Errorf("nicht geschlossenes Anführungszeichen im Kommando")
	}
	if escaped {
		return nil, fmt.Errorf("Kommando endet mit einem einzelnen Backslash")
	}
	if current.Len() > 0 || offen {
		tokens = append(tokens, current.String())
	}
	return tokens, nil
}

// FormatCommandLine setzt Kommando und Argumente wieder zu einer anzeigbaren Zeile
// zusammen; Token mit Leerraum werden zitiert.
func FormatCommandLine(command string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	for _, token := range append([]string{command}, args...) {
		if token == "" || strings.ContainsAny(token, " \t\"'\\") {
			token = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(token) + `"`
		}
		parts = append(parts, token)
	}
	return strings.Join(parts, " ")
}
