(class_declaration
    name: (type_identifier) @className
    (class_heritage
        (extends_clause
            value: (identifier) @classExtends
        )?
        (implements_clause
            (
                (type_identifier) @classImplements
                ","?
            )*
        )?
    )?
    body: (class_body) @classBody
) @class

(abstract_class_declaration
    name: (type_identifier) @className
    (class_heritage
        (extends_clause
            value: (identifier) @classExtends
        )?
        (implements_clause
            (
                (type_identifier) @classImplements
                ","?
            )*
        )?
    )?
    body: (class_body) @classBody
) @abstractClass
