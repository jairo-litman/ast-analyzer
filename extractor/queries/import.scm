(import_statement
    "type"? @import.type
    (import_clause
        (
          [
              (namespace_import
                  (identifier) @import.namespace
              )
              (identifier) @import.default
              (named_imports
                  (
                      (import_specifier
                          "type"? @import.type
                          name: (identifier) @import.named
                          alias: (identifier)? @import.alias
                      )
                      ","?
                  )*
              )
          ]
          ","?
        )*
    )?
    source: (string (string_fragment) @import.source)
) @import.statement

(variable_declarator
    name: (identifier) @import.default
    value: (call_expression
        function: (identifier) @call.type
        arguments: (arguments
            (string (string_fragment) @import.source)
        )
    )
    (#eq? @call.type "require")
) @import.statement