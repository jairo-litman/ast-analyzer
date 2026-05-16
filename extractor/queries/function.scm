(function_declaration
    name: (identifier) @funcName
    parameters: (_)? @funcParams
    return_type: (type_annotation (_) @funcReturnType)?
    body: (_) @funcBody
) @funcDecl

(method_definition
    name: (property_identifier) @funcName
    parameters: (_)? @funcParams
    return_type: (type_annotation (_) @funcReturnType)?
    body: (_) @funcBody
) @funcDecl

(variable_declarator
    name: (identifier) @funcName
    value: (arrow_function
        [
            parameters: (formal_parameters) @funcParams
            parameter: (identifier) @funcParams
        ]?
        return_type: (type_annotation (_) @funcReturnType)?
        body: (_) @funcBody
    )
) @funcDecl

(public_field_definition
    name: (property_identifier) @funcName
    value: (arrow_function
        [
            parameters: (formal_parameters) @funcParams
            parameter: (identifier) @funcParams
        ]?
        return_type: (type_annotation (_) @funcReturnType)?
        body: (_) @funcBody
    )
) @funcDecl

(call_expression
    function: (parenthesized_expression
        (arrow_function
            [
                parameters: (formal_parameters) @funcParams
                parameter: (identifier) @funcParams
            ]?
            return_type: (type_annotation (_) @funcReturnType)?
            body: (_) @funcBody
        ) @funcDecl
    )
)

; Anonymous arrow functions with block bodies — typical test
; callbacks (`it('...', () => { ... })`) and other inline
; closures. Restricted to statement_block bodies so tiny
; expression-body lambdas (`x => x * 2`) don't pollute the
; symbol catalog. The variable-bound and IIFE captures above
; also match these nodes; QueryFunctions dedupes by start byte,
; preferring the named entry.
(arrow_function
    [
        parameters: (formal_parameters) @funcParams
        parameter: (identifier) @funcParams
    ]?
    return_type: (type_annotation (_) @funcReturnType)?
    body: (statement_block) @funcBody
) @funcDecl
