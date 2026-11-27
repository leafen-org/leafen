# Forms

<!-- guide-sources:
web/src/components/database/FormView.tsx
web/src/components/database/FormQuestionCard.tsx
web/src/components/database/FormShareMenu.tsx
web/src/components/database/FormSubmitSettings.tsx
web/src/components/database/FormAddQuestionMenu.tsx
web/src/components/forms/FormSurface.tsx
internal/app/form_file_uploads.go
internal/app/form_submission.go
-->

This chapter covers forms: making one, its questions, who can fill it out,
and what happens to the answers. A form is a view of a database whose
answers become the database's rows. It is for anyone who collects requests,
sign-ups or reports, and for agents that read the answers. Views in general
are in [Views](views.md) and the database's properties in
[Properties](properties.md).

## Make a form

- In a database, press **+** after the last view tab and choose **Form**.
  The new view is named **Form builder**.
- On a page, type `/` and choose **Form** ("Create a form from scratch"):
  an inline database that starts with a form view.

A new form on a database that has properties asks "Make form questions
from the properties you already have?": **Create 〈n〉 questions** makes a
question for each property a form can ask, and **Start from scratch** makes
only the title question. Forms can only be edited on a computer, not on a
phone.

## Build the form

The form builder shows the form as a respondent sees it, with:

- **Form title** and **Description (optional)**, and **Form icon** and
  **Form cover** when you hover over the top;
- a line saying who can fill it out, with **Change**;
- one card for each question, and **+** between and after them to add one.

### Questions

**+** opens **Select type**: **Text**, **Multiple choice**, **Date**,
**Person**, **Files & media**, **Number**, **Checkbox**, **Email**, **URL**,
**Phone** and **Place**. A new question adds a property of that type to the
database; it can also ask an existing property (**Existing properties**).

Each question's **⋯** opens **Question options**:

| Option | What it does |
| --- | --- |
| **Required** | the form can't be sent without an answer ("This question is required") |
| **Description** | adds a line of explanation under the question |
| **Show options as** | for multiple choice: **List** or **Dropdown** |
| **Max selection** | for multiple choice that takes several answers, and Person and Relation questions: **Unlimited** or a number |
| **Question type** | changes the question's type |
| **View linked property** | shows the property the answers go into |
| **Sync with property name** | keeps the question's wording the same as the property's name (on at first) |
| **Move question** | moves it up, down, or to the top or bottom of its group |
| **Add conditional logic** | shows other questions only when this answer has the values you pick ("Add or select questions") |
| **Duplicate question**, **Delete question** | |

A question asking a Status or Rollup property, or another type a form can't
ask, is not offered.

### Answers by question type

| Question | Answer |
| --- | --- |
| Text | words ("Your answer"); **Long answer** for several lines |
| Multiple choice | one or more of the property's options; a Select takes one |
| Date | a date, and an end date and time where the property has them |
| Person | members of the workspace ("Search for people or groups…") |
| Relation | rows of the related database ("Select pages") |
| Files & media | uploaded files: up to 20 in an answer, each up to 100 MB |
| Number, Email, URL, Phone | checked: "Invalid number", "Invalid email", "Invalid URL", "Invalid phone number" |
| Checkbox | a box, with an optional **Checkbox label** |
| Place | a point picked on a map |

Person and Relation questions are only for members of the workspace: a form
open to anyone on the web can't ask them.

## Share the form

**Share form** has:

- **Who can fill out**: **No access** (Private), **Anyone at 〈workspace〉
  with the link** (the default) or **Anyone on the web with link** (Public);
- **Anonymous responses**: off at first; on, answers don't record who sent
  them, and the form says "Submitting response anonymously";
- **Access to submission**: what a respondent can do with the row their
  answer made: **No access** (the default), **Can view**, **Can comment**,
  **Can edit** or **Full access**;
- the form's link and **Copy form link**.

The link has the form `https://〈your server〉/form/lfm_…`. A form whose
**Who can fill out** is **No access**, or that was deleted, says "The form is
no longer open, or it was deleted."

**Preview** shows the form as a respondent sees it, without sending.

## Fill out a form

A member opening the link sees "Submitting response as 〈name〉" over the
questions; someone outside the workspace sees the form only when it is open
to anyone on the web. **Submit** sends the answers and shows the
confirmation: by default "Thank you!" and "Your response was
sent.", with **View my response**.

Each answer becomes a new row of the database, with the answers in its
properties. **View my response** opens that row, as far as
**Access to submission** lets the respondent.

## Submit screen

**Settings** › **Submit screen** sets:

- **Submit button**: **Color** (**Default**, **Light gray**, **Brown**,
  **Yellow**, **Orange**, **Green**, **Blue**, **Purple**, **Pink** or
  **Red**) and **Text** ("Submit" by default);
- **Confirmation title** ("Thank you!") and **Confirmation body** ("Your
  response was sent.");
- **More buttons**: **View my response** (on), **Email me a copy** (on;
  offered to logged-in members when the server's administrator has set up
  email) and **Submit another** (off).

**Settings** also has **New question**, **Automations**, **Source** and
**Delete form**, which asks "Permanently delete 〈form〉?": "The form will
stop taking responses. Responses already submitted are kept".

## Names, limits and defaults

| Name | Limit or default | Notes |
| --- | --- | --- |
| **Who can fill out** | Anyone at the workspace with the link | |
| **Access to submission** | No access | |
| **Anonymous responses** | off | |
| Files in one answer | 20 | |
| Size of one file | 100 MB | |
| File uploads from one respondent | 60 in 10 minutes, for each form | |
| Submit button text | "Submit" | |

## For agents and the API

An agent reads a form's answers as the database's rows, with
`notion-query-data-sources` or `notion-fetch`, and can react to new answers
with an automation ([Automations and buttons](automations-and-buttons.md))
or an event webhook ([MCP reference](../../MCP.md#event-webhooks)). The
native API's form operations start at `POST /api/leafen/v1/forms`; list them
with `GET /api/leafen/v1/operations?query=form`
([Building on Leafen](../developers.md#find-the-operations)).

## Differences from Notion

- A required answer's error clears as soon as it is filled in.
- A condition on several options names its two choices apart: **any** and
  **all**.

## Related chapters

- [Views](views.md)
- [Properties](properties.md)
- [Automations and buttons](automations-and-buttons.md)
- [Databases](databases.md)
