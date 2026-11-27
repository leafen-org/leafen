# Automations and buttons

<!-- guide-sources:
web/src/components/database/DatabaseAutomationPanel.tsx
web/src/components/database/DatabaseAutomationDialog.tsx
web/src/components/database/automationTriggerConditions.ts
web/src/components/automation/AutomationActionEditor.tsx
web/src/components/automation/automationActionModel.ts
internal/app/database_automation_wire.generated.go
internal/app/database_automation_schedule.go
internal/app/database_automation_delivery_provider.go
internal/app/automation_slack_channels.go
-->

This chapter covers database automations, which run actions when rows
change or on a schedule, and buttons, which run actions when someone presses
them: the Button block on a page and the Button property in a database. It
is for anyone who wants routine changes made for them, and for agents that
set automations up. Formulas used inside actions are described in
[Formulas](formulas.md).

## Make an automation

1. In a database, press **Automations** (the lightning bolt) in the toolbar.
   With no automations yet, the editor opens at once; otherwise the
   **Automations** list opens: press **New automation**.
2. Name the automation at the top; left empty, it is "〈your name〉's
   automation".
3. Choose which rows it watches with **For all pages in 〈database〉**: the
   **Entire data source**, or **Any page in this view** for one of the
   database's views (its filters then decide the rows).
4. Under **When**, press **New trigger** and choose one ([Triggers](#triggers)).
   **Add trigger** adds more, and **When all ▾ triggers occur** chooses
   whether **All** or **Any** of them must happen.
5. Under **Action**, press **New action** and choose one
   ([Actions](#actions)). **Add action** adds more; they run in order.
6. Press **Turn on**. "〈name〉 created" confirms it.

The **Automations** list shows each automation with a sentence that sums it
up ("When Stage set to Complete, set Done to checked"), and **Active** or
**Paused**. Press one to edit it; its menu has **Duplicate** and delete.
Automations is also in **Settings** › **Automations**.

Only people with full access to a database can make or change its
automations; content viewers see "Content viewer access doesn’t let you
create automations." A database in the trash takes no automations.

## Triggers

| Trigger | Runs when |
| --- | --- |
| **Page added** | a row is added to the watched rows |
| **Any property edited** | any property of a watched row changes; adding it replaces the property triggers ("Replace other triggers?") |
| A property, under **Property** | that property changes as the condition says |
| **Every…** | on a schedule |

A property trigger's conditions depend on its type:

| Property types | Conditions |
| --- | --- |
| Select, Status | the options it is set to (all ticked at first), **Is**, **Is not**, **Is edited**, **Is cleared** |
| Multi-select, Person, Relation | **Contains**, **Does not contain**, **Is edited**, **Is cleared** |
| Number | **=**, **≠**, **>**, **<**, **≥**, **≤**, **Is edited**, **Is cleared** |
| Title, Text, URL, Email, Phone | **Is set to**, **Is not set to**, **Contains**, **Does not contain**, **Starts with**, **Ends with**, **Is edited**, **Is cleared** |
| Checkbox | set to **Checked** or **Unchecked** |
| Date | any edit |

A Status trigger lists its groups and options, with **Any option** at the
top: ticking a group ticks its options, and the trigger reads like
"Stage set to Complete".

### Schedules

**Every…** runs the automation on a schedule rather than for one row:

- presets **Day**, **Weekday**, **Week**, **2 Weeks**, **Month**,
  **3 Months**, **6 Months** and **Year**, or **Every** a number of days (up
  to 365), weeks (up to 52, on chosen weekdays), months (up to 24, on a day
  of the month or a weekday of a week such as "2nd") or years (up to 10);
- **At** a time, or **Around midnight** (between 12 and 3 AM), and
  **Timezone**;
- **Starting** a date, and **Ending**: **Never**, a **Specific date**, or
  after a **Number of runs** (at most 999).

**Next occurrence:** shows when it runs next. A schedule that can never run says "This
automation will never run".

## Actions

| Action | Does |
| --- | --- |
| **Edit property…** | sets a property of the row that triggered it: a value, empty, or a **Custom formula**; a Person property can **Replace with…**, **Add…**, **Remove…** or **Toggle…** people |
| **Add page to…** | adds a row to a database (this one or another), with property values and optionally a template |
| **Edit pages in…** | changes rows of a database: all of them, or those matching a filter |
| **Archive pages…** | archives rows of a database, all or matching a filter |
| **Send notification to…** | sends a notification in Leafen's inbox to people: specific people (up to 20 by name), the people in a Person property, or a **Custom formula**; with a **Message** |
| **Send mail…** | sends an email, with a subject and body, from the server |
| **Send webhook** | sends the row's properties as JSON to an address, with custom headers |
| **Send Slack notification to…** | posts a message to a Slack channel |
| **Define variables** | names values (text, or a formula) that later actions use |
| **Run app action** | runs an action of an installed app, where an app offers one |

Messages, values and variables can mention the row's properties and these
names: **Whoever triggered** (the person whose edit ran it), **Page creator**,
**Time triggered**, **Date triggered** and **Trigger page**. A scheduled run
has no triggering person or row.

**Send webhook**'s address must be on the open internet; an address on this
computer or a private network shows "Invalid URL". An automation takes at
most 5 webhook actions. A workspace owner can turn webhook actions off in the
workspace's settings (**Allow webhook actions in automations**).

**Send Slack notification to…** lists the channels of the Slack connections
the server's administrator set up with the `LEAFEN_AUTOMATION_SLACK_CONNECTIONS`
setting (a JSON object of connection names and Slack bot tokens). Without
one, the action has no channel to choose. **Send mail…** needs the server's
email to be set up.

### When an action fails

An action that can't run marks its step red ("Fix any errors or remove
failing steps…") and the automation can't be turned on until it is fixed. A
running automation that fails is **Paused** with the reason, for example
"An action can't run — it may use a deleted property or database." or "A
notification, email or webhook couldn't be sent after several tries."
Notifications, emails and webhooks are tried up to 5 times, waiting from
30 seconds up to 30 minutes between tries. An automation runs with the
access of whoever last saved or turned it on; if that person can no longer
edit the database, it pauses until someone who can turns it back on.

## Buttons

### The Button block

1. On a page, type `/button` and choose **Button** ("Run custom
   automations with a click").
2. Name it (it shows "New button" until you do) and choose its icon.
3. Under **Actions**, press **New action**. A Button block offers
   **Insert blocks**, **Add page to…**, **Edit pages in…**,
   **Send notification to…**, **Send mail…**, **Send webhook**,
   **Archive pages…**, **Show confirmation**, **Open page, form, or URL**,
   **Send Slack notification to…** and **Define variables**. The three it has
   that automations don't:
   - **Insert blocks**: adds blocks you write **Above button**,
     **Below button**, **At top of page** or **At bottom of page**;
   - **Show confirmation**: asks before the next actions run, with a
     **Confirmation message** ("Continue?"), and **Continue** and **Cancel**
     button texts;
   - **Open page, form, or URL**: opens a page, a form or an address.
4. Press **Done**.

Pressing the button runs its actions in order. **Whoever clicked** and
**This page** name the person and the page in its messages and values.

### The Button property

A Button property puts a button in every row of a database. Add it like any
property ([Properties](properties.md#button-button)), then press
**Edit automation** in its column menu to choose its actions, as for a Button
block. Pressing a row's button runs the actions for that row: **This page**
is the row.

## Names, limits and defaults

| Name | Limit or default | Notes |
| --- | --- | --- |
| Automation name | up to 200 characters | Left empty: "〈your name〉's automation". |
| Webhook actions in an automation | 5 | |
| People named in one notification | 20 | A Person property sends to everyone in it. |
| Schedule interval | 365 days, 52 weeks, 24 months, 10 years | |
| Scheduled runs | at most 999 | |
| Delivery tries | 5 | 30 seconds to 30 minutes apart. |
| Trigger values | 100 per condition | |
| Slack connections | 100 | |

## For agents and the API

An agent with the `structure` scope manages a database's automations through
the native API, for example `GET /api/leafen/v1/pages/{pageId}/databases/{databaseId}/automations`
to list them and `POST /api/leafen/v1/pages/{pageId}/databases/{databaseId}/automations`
to make one; `leafen_operations` lists these operations with their contracts,
and `leafen_call` runs them. A Button block runs with
`POST /api/leafen/v1/pages/{pageId}/buttons/{blockId}/execute`. See
[Building on Leafen](../developers.md#find-the-operations).

## Differences from Notion

- **Send mail…** sends from the server itself, not through a Gmail account,
  and is marked with an envelope.
- A webhook to this computer or a private network is refused, and a button
  that has one opens its settings instead of running.
- An automation's summary names the property a person action changes, and a
  card for a deleted property names the property.
- People automations can't edit members' own properties (name, about,
  membership type).
- The automations list has no help card about retried triggers.

## Related chapters

- [Formulas](formulas.md)
- [Properties](properties.md)
- [Forms](forms.md)
- [Databases](databases.md)
