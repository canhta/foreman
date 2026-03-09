{% if retry_error_type == "compile" %}
Focus on fixing the build error. Check import paths, undefined symbols, and missing return statements. Do not refactor unrelated code.
{% elif retry_error_type == "type_error" %}
Focus on fixing the type mismatch. Verify interface implementations, check function signatures, and ensure correct type assertions.
{% elif retry_error_type == "lint_style" %}
Focus on fixing the lint/style issues listed below. Do not rewrite working logic.
{% elif retry_error_type == "test_assertion" %}
Focus on making the failing test assertions pass. Read the expected vs actual values carefully and adjust implementation, not tests.
{% elif retry_error_type == "test_runtime" %}
Focus on preventing the runtime panic. Check nil pointer dereferences, slice/map bounds, and error returns before use.
{% elif retry_error_type == "spec_violation" %}
Focus on satisfying the acceptance criteria listed below. Do not change code unrelated to the failing criteria.
{% elif retry_error_type == "quality_concern" %}
Focus on addressing the quality concerns listed below. Refactor only the flagged areas.
{% endif %}
