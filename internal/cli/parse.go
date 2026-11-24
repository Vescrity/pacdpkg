package cli

import (
	"fmt"
	"strings"
)

// Token is a parsed command-line option occurrence. Defs holds every
// definition that matches the token; which one applies depends on the
// operation, so resolution happens after the operation is known.
type Token struct {
	Defs []*OptDef
	Arg  string
	Raw  string
}

// For returns the definition valid for op, or nil.
func (t *Token) For(op Op) *OptDef {
	for _, d := range t.Defs {
		if d.Ops.Has(op) {
			return d
		}
	}
	return nil
}

func (t *Token) Primary() *OptDef { return t.Defs[0] }

// ParseError carries a message already formatted like getopt_long's
// diagnostics, which are prefixed with the program name.
type ParseError struct{ Msg string }

func (e *ParseError) Error() string { return e.Msg }

// Parse emulates GNU getopt_long with argument permutation, as used by
// pacman: options and targets may be interleaved, "--" ends option parsing.
func Parse(prog string, args []string) ([]Token, []string, error) {
	var tokens []Token
	var targets []string

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			targets = append(targets, args[i+1:]...)
			return tokens, targets, nil

		case strings.HasPrefix(a, "--"):
			name, val, hasVal := strings.Cut(a[2:], "=")
			defs := findLong(name)
			if len(defs) == 0 {
				matches := prefixMatches(name)
				switch len(matches) {
				case 0:
					return nil, nil, &ParseError{fmt.Sprintf("%s: unrecognized option '%s'", prog, a)}
				case 1:
					defs = findLong(matches[0])
					name = matches[0]
				default:
					var b strings.Builder
					fmt.Fprintf(&b, "%s: option '%s' is ambiguous; possibilities:", prog, a)
					for _, m := range matches {
						fmt.Fprintf(&b, " '--%s'", m)
					}
					return nil, nil, &ParseError{b.String()}
				}
			}
			def := defs[0]
			switch def.Mode {
			case ArgNone:
				if hasVal {
					return nil, nil, &ParseError{fmt.Sprintf("%s: option '--%s' doesn't allow an argument", prog, name)}
				}
			case ArgRequired:
				if !hasVal {
					if i+1 >= len(args) {
						return nil, nil, &ParseError{fmt.Sprintf("%s: option '--%s' requires an argument", prog, name)}
					}
					i++
					val = args[i]
				}
			}
			tokens = append(tokens, Token{Defs: defs, Arg: val, Raw: "--" + name})

		case len(a) > 1 && a[0] == '-':
			cluster := a[1:]
			for j := 0; j < len(cluster); j++ {
				letter := cluster[j]
				defs := findShort(rune(letter))
				if len(defs) == 0 {
					return nil, nil, &ParseError{fmt.Sprintf("%s: invalid option -- '%c'", prog, letter)}
				}
				def := defs[0]
				arg := ""
				consumesRest := false
				switch def.Mode {
				case ArgRequired:
					if j+1 < len(cluster) {
						arg = cluster[j+1:]
					} else if i+1 < len(args) {
						i++
						arg = args[i]
					} else {
						return nil, nil, &ParseError{fmt.Sprintf("%s: option requires an argument -- '%c'", prog, letter)}
					}
					consumesRest = true
				case ArgOptional:
					arg = cluster[j+1:]
					consumesRest = true
				}
				tokens = append(tokens, Token{Defs: defs, Arg: arg, Raw: "-" + string(letter)})
				if consumesRest {
					break
				}
			}

		default:
			targets = append(targets, a)
		}
	}
	return tokens, targets, nil
}

func prefixMatches(name string) []string {
	var out []string
	for _, n := range longNames() {
		if strings.HasPrefix(n, name) {
			out = append(out, n)
		}
	}
	return out
}
