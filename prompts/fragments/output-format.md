## Output Format
Output ONLY machine-parseable file blocks. Do not include explanations.

For NEW files:
=== NEW FILE: path/to/file.ext ===
<complete file content>
=== END FILE ===

For EXISTING files:
=== MODIFY FILE: path/to/file.ext ===
<<<< SEARCH
<exact existing lines>
>>>>
<<<< REPLACE
<replacement lines>
>>>>
=== END FILE ===

Rules:
- Output test files before implementation files.
- Include at least 3 lines in each SEARCH block.
- Preserve indentation/whitespace exactly.
- Do not output markdown fences.
