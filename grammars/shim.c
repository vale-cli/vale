// The host side of a grammar module: tree-sitter's core and one grammar,
// compiled to WebAssembly with this file as the only interface. Vale drives
// it through internal/lint/code/tsw.
//
// Nodes cross the boundary as handles into a table that parse() resets, so
// a handle is good until the next parse. Strings come back as a pointer,
// with their length from last_len(). Query matches are written into one
// buffer that the host reads whole.
#include <stdlib.h>
#include <string.h>
#include <tree_sitter/api.h>

#define EXPORT(name) __attribute__((export_name(#name)))

const TSLanguage *TS_LANG(void);

static TSParser *parser;
static TSTree *tree;

static TSNode *nodes;
static uint32_t node_len, node_cap;

static TSQuery **queries;
static uint32_t query_len, query_cap;

static uint32_t *words; // match and predicate output
static uint32_t word_len, word_cap;

static uint32_t last;
static uint32_t query_err;

static int32_t push_node(TSNode n) {
  if (ts_node_is_null(n)) return -1;
  if (node_len == node_cap) {
    node_cap = node_cap ? node_cap * 2 : 1024;
    nodes = realloc(nodes, node_cap * sizeof(TSNode));
  }
  nodes[node_len] = n;
  return (int32_t)node_len++;
}

static void push_word(uint32_t w) {
  if (word_len == word_cap) {
    word_cap = word_cap ? word_cap * 2 : 1024;
    words = realloc(words, word_cap * sizeof(uint32_t));
  }
  words[word_len++] = w;
}

EXPORT(alloc) void *shim_alloc(size_t n) { return malloc(n); }
EXPORT(release) void shim_release(void *p) { free(p); }
EXPORT(last_len) uint32_t last_len(void) { return last; }

// parse reads src and returns the root node's handle, 0.
EXPORT(parse) int32_t parse(const char *src, uint32_t len) {
  if (!parser) {
    parser = ts_parser_new();
    ts_parser_set_language(parser, TS_LANG());
  }
  if (tree) ts_tree_delete(tree);
  tree = ts_parser_parse_string(parser, NULL, src, len);
  node_len = 0;
  return push_node(ts_tree_root_node(tree));
}

EXPORT(node_type) const char *node_type(int32_t h) {
  const char *t = ts_node_type(nodes[h]);
  last = (uint32_t)strlen(t);
  return t;
}
EXPORT(node_start_byte) uint32_t node_start_byte(int32_t h) { return ts_node_start_byte(nodes[h]); }
EXPORT(node_end_byte) uint32_t node_end_byte(int32_t h) { return ts_node_end_byte(nodes[h]); }
EXPORT(node_start_row) uint32_t node_start_row(int32_t h) { return ts_node_start_point(nodes[h]).row; }
EXPORT(node_start_col) uint32_t node_start_col(int32_t h) { return ts_node_start_point(nodes[h]).column; }
EXPORT(node_named_child_count) uint32_t node_named_child_count(int32_t h) { return ts_node_named_child_count(nodes[h]); }
EXPORT(node_named_child) int32_t node_named_child(int32_t h, uint32_t i) { return push_node(ts_node_named_child(nodes[h], i)); }
EXPORT(node_next_named_sibling) int32_t node_next_named_sibling(int32_t h) { return push_node(ts_node_next_named_sibling(nodes[h])); }
EXPORT(node_child_by_field_name) int32_t node_child_by_field_name(int32_t h, const char *name, uint32_t len) {
  return push_node(ts_node_child_by_field_name(nodes[h], name, len));
}
// node_sexp is the node as an S-expression; the host releases it.
EXPORT(node_sexp) char *node_sexp(int32_t h) {
  char *s = ts_node_string(nodes[h]);
  last = (uint32_t)strlen(s);
  return s;
}

// query_new compiles expr and returns its handle, or -1 with query_error()
// set to the offset and type tree-sitter reported.
EXPORT(query_new) int32_t query_new(const char *expr, uint32_t len) {
  uint32_t offset; TSQueryError type;
  TSQuery *q = ts_query_new(TS_LANG(), expr, len, &offset, &type);
  if (!q) { last = offset; query_err = (uint32_t)type; return -1; }
  if (query_len == query_cap) {
    query_cap = query_cap ? query_cap * 2 : 16;
    queries = realloc(queries, query_cap * sizeof(TSQuery *));
  }
  queries[query_len] = q;
  return (int32_t)query_len++;
}
EXPORT(query_error) uint32_t query_error(void) { return query_err; }
EXPORT(query_capture_count) uint32_t query_capture_count(int32_t q) { return ts_query_capture_count(queries[q]); }
EXPORT(query_capture_name) const char *query_capture_name(int32_t q, uint32_t i) {
  return ts_query_capture_name_for_id(queries[q], i, &last);
}
EXPORT(query_string_value) const char *query_string_value(int32_t q, uint32_t i) {
  return ts_query_string_value_for_id(queries[q], i, &last);
}
EXPORT(query_pattern_count) uint32_t query_pattern_count(int32_t q) { return ts_query_pattern_count(queries[q]); }

// query_predicates writes the predicate steps of one pattern to the word
// buffer as (type, value) pairs and returns how many steps there are.
EXPORT(query_predicates) uint32_t query_predicates(int32_t q, uint32_t pattern) {
  uint32_t n;
  const TSQueryPredicateStep *steps = ts_query_predicates_for_pattern(queries[q], pattern, &n);
  word_len = 0;
  for (uint32_t i = 0; i < n; i++) { push_word((uint32_t)steps[i].type); push_word(steps[i].value_id); }
  return n;
}
EXPORT(words_ptr) uint32_t *words_ptr(void) { return words; }
EXPORT(words_len) uint32_t words_len(void) { return word_len; }

// query_exec runs q over the node and writes every match to the word
// buffer: pattern index, capture count, then (capture index, node handle)
// pairs; it returns the number of matches.
EXPORT(query_exec) uint32_t query_exec(int32_t q, int32_t h) {
  TSQueryCursor *cur = ts_query_cursor_new();
  ts_query_cursor_exec(cur, queries[q], nodes[h]);
  word_len = 0;
  uint32_t count = 0;
  TSQueryMatch m;
  while (ts_query_cursor_next_match(cur, &m)) {
    push_word(m.pattern_index);
    push_word(m.capture_count);
    for (uint16_t i = 0; i < m.capture_count; i++) {
      push_word(m.captures[i].index);
      push_word((uint32_t)push_node(m.captures[i].node));
    }
    count++;
  }
  ts_query_cursor_delete(cur);
  return count;
}

// tree.c reaches for dup() only to print DOT graphs, which never happens here.
int dup(int fd) { (void)fd; return -1; }
