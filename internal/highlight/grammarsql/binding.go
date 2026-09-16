package grammarsql

/*
#cgo CFLAGS: -std=c11 -fPIC -I./src
#include "src/parser.c"
#include "src/scanner.c"
*/
import "C"
import "unsafe"

// Language returns the generated Tree-sitter language pointer.
func Language() unsafe.Pointer { return unsafe.Pointer(C.tree_sitter_sql()) }
