# Branch TUI

A terminal view of the user's Jira tickets with their Bitbucket branches and pull requests, plus quick actions on them.

## Language

### Tickets

**Ticket**:
A Jira issue shown in the list.
_Avoid_: Issue, task, story

**Ticket key**:
The Jira identifier of a ticket, such as `PROJ-123`.
_Avoid_: Key (alone), ID

**Status**:
The workflow state a ticket is currently in, such as "In Progress".
_Avoid_: State, column

**Transition**:
A move that takes a ticket from its current status to another one; only the transitions Jira offers for that ticket are possible.
_Avoid_: Status change, status update

**Assignee**:
The single user responsible for a ticket; a ticket may have none.
_Avoid_: Owner, user

### Interaction

**Action**:
A named operation the user runs on the selected ticket or branch, such as copying a branch name or transitioning a ticket.
_Avoid_: Command, shortcut

**Binding**:
The keyboard key that runs an action; users can override it in config.
_Avoid_: Key (alone), hotkey, shortcut

**Reserved key**:
A navigation key that no action may be bound to.
_Avoid_: System key

### Branches and pull requests

**Source branch**:
The ticket's own branch, which a pull request merges from.
_Avoid_: Feature branch, head

**Target branch**:
The branch a pull request merges into, such as `master`, `uat` or `qa`.
_Avoid_: Destination, base, dest
