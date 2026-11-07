# {{feature title}} — interaction flows

## Primary scenarios

1. {{start state}} → {{event / guard / command}} → {{next state}} → {{outcome}}

## State transition matrix

| From | Event | Guard | Command | To | Observable result |
| --- | --- | --- | --- | --- | --- |
| {{state-id}} | {{event-id}} | {{guard-id or none}} | {{command-id or none}} | {{state-id}} | {{result}} |

## Error, cancellation, retry, and exit paths

Describe every applicable alternate path. `machine.json` is authoritative.
