(export_statement
    "type"? @reexport.type
    (export_clause
        (
            (export_specifier
                "type"? @reexport.specifier_type
                name: (identifier) @reexport.name
                alias: (identifier)? @reexport.alias
            )
            ","?
        )*
    )?
    (namespace_export
        (identifier) @reexport.namespace
    )?
    source: (string (string_fragment) @reexport.source)
) @reexport
