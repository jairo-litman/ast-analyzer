(export_statement
    "default"
    declaration: (function_declaration
        name: (identifier) @default.name
    )
)

(export_statement
    "default"
    declaration: (class_declaration
        name: (type_identifier) @default.name
    )
)

(export_statement
    "default"
    value: (identifier) @default.name
)
