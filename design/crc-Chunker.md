# Chunker
**Requirements:** R10, R11, R12, R13

Executes external chunking commands. A chunking strategy is a name mapped to a shell command. The command receives a filename and outputs byte offsets (one per line) that define chunk boundaries.

## Knows
- (stateless — strategies stored in DB settings)

## Does
- Run(cmd, filepath): execute `cmd filepath`, parse stdout as list of byte offsets, return []int64
- Validate(cmd): verify command exists and is executable

## Collaborators
- none (leaf type, executes external processes)

## Sequences
- seq-add.md
