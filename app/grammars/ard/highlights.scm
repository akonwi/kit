; Ard highlights (tree-sitter CLI)

; Keywords (node-based)
(break_statement) @keyword
(defer_statement "defer" @keyword)
(if_statement "if" @keyword)
(while_loop "while" @keyword)
(for_loop "for" @keyword)
(for_in_loop "for" @keyword)
(for_in_loop "in" @keyword)
(for_in_loop (for_binding) @variable.parameter)
(match_expression "match" @keyword)
(select_expression "select" @keyword)
(select_case "let" @keyword)
(select_case binding: (identifier) @variable.parameter)
(try_expression "try" @keyword)
(unsafe_block "unsafe" @keyword)
(import_statement "use" @keyword)
(import_statement "as" @keyword)
(import_statement path: (module_path) @string.special)
(variable_declaration "let" @keyword)
(variable_declaration "mut" @keyword)
(mutable_type "mut" @keyword)
(function_declaration "test" @keyword)
(function_declaration "fn" @keyword)
(function_declaration
  _ @keyword
  (#eq? @keyword "mut"))
(extern_function "extern" @keyword)
(extern_function "fn" @keyword)
(struct_declaration "struct" @keyword)
(enum_declaration "enum" @keyword)
(trait_declaration "trait" @keyword)
(impl_block "impl" @keyword)
(impl_block "for" @keyword)
(impl_block "as" @keyword)

; Types
(primitive_type) @type.builtin
(type_parameter) @type.parameter
(type_parameters "<" @punctuation.bracket ">" @punctuation.bracket)
(type_arguments "<" @punctuation.bracket ">" @punctuation.bracket)
(generic_type (qualified_identifier) @type)
(generic_type (identifier) @type)
(qualified_identifier) @type

; Declarations
(struct_declaration name: (identifier) @type)
(enum_declaration name: (identifier) @type)
(trait_declaration name: (identifier) @type.interface)
(function_declaration name: (identifier) @function.definition)
(function_declaration name: (qualified_identifier) @function.definition)
(extern_function name: (identifier) @function.definition)
(extern_binding (qualified_identifier) @function)
(extern_binding_entry target: (extern_binding_target) @variable.builtin)
(enum_variant name: (identifier) @constant)

; Parameters
(parameter name: (identifier) @variable.parameter)
(named_argument name: (identifier) @variable.parameter)

; Variables and properties
(variable_declaration name: (identifier) @variable)
(struct_field name: (identifier) @property)
(struct_literal_field name: (identifier) @property)
(self_expression) @variable.builtin

; Member/property vs call (disabled for now; was causing CLI query errors)

; Literals
(number) @number
(string) @string
(rune) @string
(string_interpolation "{" @punctuation.bracket "}" @punctuation.bracket)
(string_content) @string
(escape_sequence) @string.escape
(brace_escape) @string.escape
(boolean) @boolean
(void) @constant.builtin
(wildcard) @variable.special

; Comments
(comment) @comment

; Punctuation
[
  "("
  ")"
  "["
  "]"
  "{"
  "}"
  ","
  ":"
] @punctuation.delimiter

[
  "."
  "::"
  "=>"
  "->"
] @punctuation.special

; Operators
[
  "="
  "=+"
  "=-"
  "+"
  "-"
  "*"
  "/"
  "%"
  "<"
  "<="
  ">"
  ">="
  "=="
  "!="
  "!"
  "?"
  ".."
] @operator
