package hook

import "strings"

// Detail puts an agent's own word into the shared detail namespace, which is
// lowercase kebab by convention (§A5.8). Details are unprefixed and shared
// across agents, so `permission-prompt` means the same thing whoever wrote it,
// and the spelling has to be made the same way for every agent: `ServerError`,
// `server_error` and `server error` all come out `server-error`. Core never
// interprets a detail (R7), so the convention is enforced here, once, rather
// than in each adapter with its own idea of it.
func Detail(word string) string {
	var out strings.Builder
	for i, letter := range strings.TrimSpace(word) {
		switch {
		case letter >= 'A' && letter <= 'Z':
			if i > 0 {
				out.WriteByte('-')
			}
			out.WriteRune(letter + 'a' - 'A')
		case letter == '_' || letter == ' ':
			out.WriteByte('-')
		default:
			out.WriteRune(letter)
		}
	}
	return out.String()
}
