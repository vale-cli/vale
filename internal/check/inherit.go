package check

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/system"
)

// A rule may extend another rule instead of an extension point: an `extends`
// value containing a dot is a rule reference, resolved against the styles on
// the search path. The child starts from the parent's definition and lays its
// own keys on top, so two styles can share one carefully built pattern and
// disagree only about message, level, or a handful of entries. See #386 for
// the config layer of the same idea.
//
// Overlay semantics, chosen once and used everywhere:
//
//   - A bare key replaces the parent's value wholesale.
//   - `key+` appends to a parent list, or merges into a parent map with the
//     child's entries winning.
//   - `key-` removes entries from a parent list by their source text, or the
//     named keys from a parent map. Removing something the parent does not
//     have is an error: if upstream renames or drops an entry a child was
//     removing, the child's author hears about it at compile time instead of
//     silently diverging.
//   - A scalar takes only the bare form, and a file that writes both `key`
//     and `key+`/`key-` has said "replace" and "edit the replacement" at
//     once, which is always a mistake.
//
// The parent must be present on the search path, not enabled: inheritance is
// a file reference, and `vale sync` is what puts the file there.

// maxInheritanceDepth bounds a chain of rule references. Deeper nesting is
// nearly always a cycle routed through distinct files.
const maxInheritanceDepth = 10

// isRuleRef reports whether an `extends` value names a rule rather than an
// extension point. Extension points are single lowercase words; rule names
// contain at least one dot.
func isRuleRef(extends string) bool {
	return strings.Contains(extends, ".")
}

// flatten resolves a rule's inheritance chain, returning the definition
// buildRule should see: the root ancestor's extension point with every
// descendant's keys overlaid in order.
func (mgr *Manager) flatten(generic baseCheck, path string, seen []string) (baseCheck, error) {
	extends, ok := generic["extends"].(string)
	if !ok || !isRuleRef(extends) {
		// A base rule has no parent for `+`/`-` to act on.
		for key := range generic {
			if key == "" {
				continue
			}
			if op := key[len(key)-1]; op == '+' || op == '-' {
				return nil, core.NewE201FromPosition(fmt.Sprintf(
					"'%s' needs a parent: only a rule that extends another rule can use '+' or '-'", key),
					path, 1)
			}
		}
		return generic, nil
	}

	if strings.HasPrefix(extends, "Vale.") {
		return nil, core.NewE201FromPosition(fmt.Sprintf(
			"'%s' is a built-in rule generated at runtime; it has no file to extend", extends),
			path, 1)
	}

	if len(seen) >= maxInheritanceDepth {
		return nil, core.NewE201FromPosition(fmt.Sprintf(
			"inheritance chain is over %d rules deep at '%s'; is there a cycle?",
			maxInheritanceDepth, extends), path, 1)
	}
	for _, name := range seen {
		if name == extends {
			return nil, core.NewE201FromPosition(fmt.Sprintf(
				"inheritance cycle: '%s' is already in this chain", extends), path, 1)
		}
	}

	parentPath, err := mgr.resolveRule(extends)
	if err != nil {
		return nil, core.NewE201FromPosition(err.Error(), path, 1)
	}

	data, err := os.ReadFile(parentPath)
	if err != nil {
		return nil, core.NewE201FromPosition(err.Error(), parentPath, 1)
	}

	parent, err := parse(data, parentPath)
	if err != nil {
		return nil, err
	}

	parent, err = mgr.flatten(parent, parentPath, append(seen, extends))
	if err != nil {
		return nil, err
	}

	// A parent's cases assert the parent's behavior; a child that changes a
	// threshold or a token list has to bring its own.
	delete(parent, "tests")

	return overlay(parent, generic, path)
}

// resolveRule maps a rule name to its file: the first segment is a style
// directory on the search path, and every segment after it is a path
// component -- the same reading loading gives names.
func (mgr *Manager) resolveRule(name string) (string, error) {
	parts := strings.Split(name, ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("'%s' is not a rule name", name)
	}

	rel := filepath.Join(parts[1:]...) + ".yml"
	for _, p := range mgr.Config.SearchPaths() {
		candidate := filepath.Join(p, parts[0], rel)
		if system.FileExists(candidate) {
			return candidate, nil
		}
	}

	return "", fmt.Errorf(
		"'%s' does not exist on the search path (looked for '%s')",
		name, filepath.Join(parts[0], rel))
}

// overlay lays a child's keys over its parent's definition.
func overlay(parent, child baseCheck, path string) (baseCheck, error) {
	out := baseCheck{}
	for k, v := range parent {
		out[k] = v
	}

	for key, v := range child {
		base := strings.TrimRight(key, "+-")
		if base != key {
			if _, both := child[base]; both {
				return nil, core.NewE201FromPosition(fmt.Sprintf(
					"'%s' both replaces '%s' and edits it; pick one", key, base), path, 1)
			}

			edited, err := editField(out[base], v, key)
			if err != nil {
				return nil, core.NewE201FromPosition(err.Error(), path, 1)
			}
			out[base] = edited
			continue
		}

		if key == "extends" {
			// The reference is consumed here; buildRule sees the root
			// ancestor's extension point.
			continue
		}
		out[key] = v
	}

	return out, nil
}

// editField applies one `key+` or `key-` operation to a parent's value.
func editField(parent, value interface{}, key string) (interface{}, error) {
	add := strings.HasSuffix(key, "+")

	switch have := parent.(type) {
	case []interface{}:
		entries, ok := value.([]interface{})
		if !ok {
			return nil, fmt.Errorf("'%s' needs a list", key)
		}
		if add {
			return append(append([]interface{}{}, have...), entries...), nil
		}
		return removeEntries(have, entries, key)
	case map[string]interface{}:
		if add {
			entries, ok := value.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("'%s' needs a mapping", key)
			}
			merged := map[string]interface{}{}
			for k, v := range have {
				merged[k] = v
			}
			for k, v := range entries {
				merged[k] = v
			}
			return merged, nil
		}

		names, ok := value.([]interface{})
		if !ok {
			return nil, fmt.Errorf("'%s' needs a list of keys to remove", key)
		}
		merged := map[string]interface{}{}
		for k, v := range have {
			merged[k] = v
		}
		for _, name := range names {
			k, _ := name.(string)
			if _, found := merged[k]; !found {
				return nil, fmt.Errorf(
					"'%s': the parent has no entry '%v' to remove", key, name)
			}
			delete(merged, k)
		}
		return merged, nil
	case nil:
		return nil, fmt.Errorf("'%s': the parent has no such field", key)
	default:
		return nil, fmt.Errorf(
			"'%s': '%s' is a single value; assign it directly instead",
			key, strings.TrimRight(key, "+-"))
	}
}

func removeEntries(have, entries []interface{}, key string) ([]interface{}, error) {
	out := append([]interface{}{}, have...)
	for _, entry := range entries {
		found := -1
		for i, existing := range out {
			if reflect.DeepEqual(existing, entry) {
				found = i
				break
			}
		}
		if found < 0 {
			return nil, fmt.Errorf(
				"'%s': the parent has no entry '%v' to remove", key, entry)
		}
		out = append(out[:found], out[found+1:]...)
	}
	return out, nil
}
