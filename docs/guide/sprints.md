# Sprints

<!-- guide-sources:
web/src/components/database/DatabaseSprintScreen.tsx
web/src/components/database/DatabaseSprintCompleteDialog.tsx
web/src/components/database/DatabaseTaskConvertDialog.tsx
web/src/components/database/DatabaseTaskSettings.tsx
internal/app/database_sprints.go
internal/app/database_sprint_complete.go
internal/app/database_sprint_deletion.go
-->

This chapter covers task databases and sprints: turning a database into a
task database, turning sprints on, planning and completing sprints, and
turning them off. It is for teams that work in fixed periods of a week or
more, and for agents that move tasks between sprints. Databases in general
are in [Databases](databases.md) and board views in [Views](views.md).

## Task databases

Sprints work on a task database: a database whose status, assignee and due
date properties are known. A database made from **Tasks Tracker**, or by
MCP with `database_type` `tasks`, is one already.

To turn another database into one:

1. Open **Settings** › **Turn into** › **Turn into Tasks database**.
2. The window explains that "A task database needs three properties:
   status, assignee and due date." Choose the property for each: **Status**,
   **Assignee** and **Due date** ("Select…", or **New** to add one). A Select
   chosen as the status is converted ("Converting turns this property into a
   Status property.").
3. Press **Save changes**. "Turned into task database" confirms it, with
   **Go to My Tasks**: its tasks now show in My tasks.

While it is a task database, those three properties keep their types
("Tasks need this property"). **Settings** › **Undo Task database** asks
"Undo Task Database?" ("My tasks and Sprints will stop using the tasks in
this database.") and, with **Remove**, makes it an ordinary database;
sprints must be turned off first.

## Turn on sprints

1. In the task database, open **Settings** › **More settings** › **Sprints**.
   **Set up sprints** explains what is added "to your teamspace":
   - **Sprint board**, a page "To manage tasks and plan your work";
   - **Sprints database**, "A record of what got done in each past
     sprint".
2. Press **Turn on sprints**. The Sprint board opens.

The task database gains a **Sprint** relation to the Sprints database. The
Sprints database holds one row per sprint, with its name, status (current,
next, last, past or future) and dates, and its tasks.

## The Sprint board

The Sprint board is a page with a board of the tasks and three tabs:

- **Current: 〈sprint〉** — the tasks of the sprint under way, by status,
  with "〈done〉 / 〈total〉 tasks" and **Complete sprint**;
- **Sprint planning** — the tasks grouped by sprint, to plan what goes into
  the next ones;
- **Backlog** — the tasks in no sprint.

Turning sprints on makes "Sprint 1", "Sprint 2" and "Sprint 3"; each later
sprint is named with its number, "Sprint 4" and on.

## Sprint settings

**Settings** › **More settings** › **Sprints** on the task database or the
Sprint board shows "Next sprint starts in 〈n〉 days (〈date〉)" and:

| Setting | Choices | Default |
| --- | --- | --- |
| **Duration** | 1 to 8 weeks | 2 weeks |
| **Start on** | a day of the week | Monday |
| **Incomplete tasks** | **Next sprint**, **Backlog** or **Current sprint**: where a finished sprint's unfinished tasks go | **Next sprint** |
| **Automated sprints** | "Each sprint completes automatically just before the next one starts." | off |

A view of the tasks can also show the current sprint with
**Manage sprint from this view** ("Show the current sprint in this view if
the Current sprint view was deleted.").

## Complete a sprint

1. Press **Complete sprint** on the Sprint board.
2. The "〈sprint〉 summary" shows its dates and "〈done〉 / 〈total〉 total
   tasks", the **Next sprint** and its **Start and end dates** (which you can
   change, or choose **No dates**), and, when tasks are unfinished, where
   they go: **Move to next sprint**, **Move to backlog** or
   **Keep in current sprint**.
3. Press **Complete 〈sprint〉**.

The current sprint becomes the last one, the next one becomes current, and a
new next sprint is planned. With **Automated sprints** on, this happens by
itself before the next sprint starts, unfinished tasks going where
**Incomplete tasks** says.

## Turn off sprints

**Settings** › **More settings** › **Sprints** › **Turn off sprints** asks
"Turn off sprints?": "The Sprint board and Sprints database are permanently
deleted and tasks lose their Sprint property. This can't be undone. Turning
sprints on again starts fresh." **Turn off and delete** does it. While a
legal hold protects the sprint pages, sprints can't be turned off ("Release
the hold, then turn sprints off.").

## Names, limits and defaults

| Name | Limit or default | Notes |
| --- | --- | --- |
| Sprint length | 1 to 8 weeks | 2 weeks at first |
| Sprint start | a day of the week | Monday at first |
| Unfinished tasks | Next sprint | |
| Automated sprints | off | |
| Sprint names | "Sprint 〈number〉" | |

## For agents and the API

An agent moves a task into a sprint by setting its **Sprint** relation with
`notion-update-page`, and reads sprints as the Sprints database's rows with
`notion-query-data-sources`. Turning sprints on and off, their settings and
completing a sprint are the native operations at
`GET /api/leafen/v1/pages/{pageId}/databases/{dataSourceId}/sprints` and
`POST /api/leafen/v1/pages/{pageId}/databases/{dataSourceId}/sprints`; list
them with `leafen_operations`
([Building on Leafen](../developers.md#find-the-operations)).

## Differences from Notion

- Sprints made after the first three are named with their number, so each
  can be told apart.
- Turning sprints off says plainly that the board and the Sprints database
  are deleted for good.
- Leaving an empty new card on the Sprint board removes it.
- A sprint action the server didn't answer says so, and the settings are
  read again once the server answers.

## Related chapters

- [Databases](databases.md)
- [Views](views.md)
- [Relations and rollups](relations-and-rollups.md)
- [Database templates](database-templates.md)
