(interface_declaration
    name: (type_identifier) @interfaceName
    (extends_type_clause
        (
            (type_identifier) @interfaceExtends
            ","?
        )*
    )?
    body: (interface_body) @interfaceBody
) @interface
