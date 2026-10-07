#!/usr/bin/env python3
"""Deterministic goctl Swagger extensions; sources are .api and declared overrides.

goctl v1.9.2 cannot express multipart.FileHeader in .api and ignores omitempty
when calculating response required fields. No handwritten Go artifact is changed.
"""
import json
from pathlib import Path
import re
import sys


def normalize(swagger, types, overrides):
    swagger['schemes'] = overrides['schemes']
    for path, methods in swagger['paths'].items():
        for method, operation in methods.items():
            if not isinstance(operation, dict) or 'responses' not in operation:
                continue
            operation['schemes'] = overrides['schemes']
            statuses = overrides['error_operations'].get(path, {}).get(method, [])
            for status in statuses:
                description = overrides['errors'][status]
                operation['responses'][status] = {
                    'description': description,
                    'schema': {'type': 'object', 'required': ['code', 'error'],
                               'properties': {'code': {'type': 'string'}, 'error': {'type': 'string'},
                                              'request_id': {'type': 'string'}}}}
    upload = overrides['multipart']
    operation = swagger['paths'][upload['path']][upload['method']]
    operation['consumes'] = ['multipart/form-data']
    operation['parameters'] = [param for param in operation['parameters'] if param['name'] != upload['field']]
    operation['parameters'].append({'in': 'formData', 'name': upload['field'], 'type': 'file',
                                    'required': True, 'description': upload['description']})
    # Match generated struct JSON-property shapes, not global property names:
    # e.g. role may be optional on a subject but required on a user response.
    shapes = {}
    for body in re.findall(r'type \w+ struct \{(.*?)\n\}', types, re.S):
        tags = re.findall(r'`json:"([^"\n]+)"`', body)
        fields = frozenset(tag.split(',')[0] for tag in tags)
        omitted = frozenset(tag.split(',')[0] for tag in tags if 'omitempty' in tag.split(',')[1:])
        if fields in shapes and shapes[fields] != omitted:
            # Only ambiguous nonempty omissions are unsafe to infer.
            if omitted or shapes[fields]:
                raise ValueError(f'ambiguous response shape: {sorted(fields)}')
        shapes[fields] = omitted

    def fix_response(schema):
        if not isinstance(schema, dict):
            return
        properties = schema.get('properties', {})
        omitted = shapes.get(frozenset(properties), frozenset())
        if omitted and 'required' in schema:
            schema['required'] = [field for field in schema['required'] if field not in omitted]
            if not schema['required']:
                del schema['required']
        for value in properties.values():
            fix_response(value)
        fix_response(schema.get('items'))
        fix_response(schema.get('additionalProperties'))
    for methods in swagger['paths'].values():
        for operation in methods.values():
            if isinstance(operation, dict):
                for response in operation.get('responses', {}).values():
                    fix_response(response.get('schema'))
    return swagger


if __name__ == '__main__':
    output, types, overrides = map(Path, sys.argv[1:])
    result = normalize(json.loads(output.read_text()), types.read_text(), json.loads(overrides.read_text()))
    output.write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n')
