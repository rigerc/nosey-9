# {{feature title}} — plan reviews and decisions

## Plan review and approval

Record **conversation** plans (not generated file drafts). Keep the initial plan and meaningful revisions; the latest version must have explicit user approval before generated artifacts are authored.

| Plan version | User feedback / decisions | What changed | Review status |
| --- | --- | --- | --- |
| v0 | {{initial plan summary}} | Initial provisional scope | Proposed |
| v1 | {{feedback or delegated choices}} | {{plan delta}} | {{approved by user / revision requested}} |

Final approved version: {{version and explicit user approval / date}}.

## Question and answer log

Use the ID form `Q-<number>` (for example `Q-1`). `evidence.json` claims classified `user-confirmed` cite these IDs in their rationale.

| ID | Open question | Answer or delegated choice | Source (user/delegated) | Contract impact |
| --- | --- | --- | --- | --- |
| Q-1 | {{question}} | {{answer}} | {{source}} | {{affected IDs}} |

## Documented assumptions

- {{explicitly delegated assumption; remove if none}}

## Unresolved questions

None when manifest status is `ready`; otherwise list each blocker and keep status `draft`.

## Change log

- {{version or date}} — {{relevant decisions / breaking interface changes}}
