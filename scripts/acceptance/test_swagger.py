"""Regression for declared Swagger additions, not handwritten generated JSON."""
import copy
import importlib.util
import json
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('normalize_swagger', ROOT / 'scripts/normalize-swagger.py')
normalizer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(normalizer)


class SwaggerTests(unittest.TestCase):
    def setUp(self):
        self.api = ROOT / 'internal/transport/restapi'
        self.swagger = json.loads((self.api / 'restapi.json').read_text())
        self.overrides = json.loads((self.api / 'docs/swagger-overrides.json').read_text())
        self.types = (self.api / 'internal/types/types.go').read_text()

    def test_normalization_is_idempotent(self):
        self.assertEqual(normalizer.normalize(copy.deepcopy(self.swagger), self.types, self.overrides), self.swagger)

    def test_multipart_has_one_required_file_and_optional_visibility(self):
        op = self.swagger['paths']['/api/v1/dataset/{id}/documents/upload']['post']
        self.assertEqual(op['consumes'], ['multipart/form-data'])
        files = [p for p in op['parameters'] if p['name'] == 'file']
        self.assertEqual(len(files), 1)
        self.assertEqual((files[0]['in'], files[0]['type'], files[0]['required']), ('formData', 'file', True))
        visibility = next(p for p in op['parameters'] if p['name'] == 'visibility')
        self.assertFalse(visibility.get('required', False))

    def test_request_and_response_optionality(self):
        op = self.swagger['paths']['/api/v1/dataset/{id}/documents']['post']
        body = next(p['schema'] for p in op['parameters'] if p['in'] == 'body')
        self.assertIn('content', body['required'])
        for field in ('source', 'visibility', 'metadata'):
            self.assertNotIn(field, body['required'])
        document = op['responses']['200']['schema']['properties']['data']['items']
        self.assertIn('id', document['required'])
        self.assertNotIn('operation', document['required'])
        op = self.swagger['paths']['/api/v1/dataset/{id}/documents/{docId}']['put']
        body = next(p['schema'] for p in op['parameters'] if p['in'] == 'body')
        self.assertFalse(body.get('required'))

    def test_errors_are_scoped_to_actual_operations(self):
        paths = self.swagger['paths']
        self.assertIn('503', paths['/api/v1/dataset/{id}/search']['post']['responses'])
        self.assertNotIn('503', paths['/api/v1/dataset/{id}/reindex']['post']['responses'])
        self.assertNotIn('404', paths['/api/v1/dataset']['post']['responses'])
        for path, methods in self.overrides['error_operations'].items():
            for method, statuses in methods.items():
                for status in statuses:
                    self.assertEqual(paths[path][method]['responses'][status]['schema']['required'], ['code', 'error'])

    def test_ambiguous_response_shape_is_rejected(self):
        types = 'type A struct {\n Value string `json:"value,omitempty"`\n}\n'
        types += 'type B struct {\n Value string `json:"value"`\n}\n'
        with self.assertRaisesRegex(ValueError, 'ambiguous response shape'):
            normalizer.normalize(copy.deepcopy(self.swagger), types, self.overrides)


if __name__ == '__main__':
    unittest.main()
