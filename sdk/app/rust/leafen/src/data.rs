//! Tables: the databases the app's data setup made, read and written by
//! their columns' names. A table is found by its setup step's key in the
//! organization a call acts in; its rows are read with Leafen's database
//! query (conditions, order and search as a view applies them, formulas
//! worked out) and written as a person's edit of a row is, so a change keeps
//! the row's place and every value it doesn't name. The Go helper's `Table`
//! does the same.

use crate::json::Value;
use crate::{api, now, random_hex};

/// One of the app's databases.
#[derive(Clone, Debug)]
pub struct Table {
    pub tenancy: String,
    pub key: String,
    pub page_id: String,
    pub data_source_id: String,
    /// Columns in the table's order.
    pub columns: Vec<Column>,
}

/// One of a table's properties; `kind` is its type.
#[derive(Clone, Debug)]
pub struct Column {
    pub id: String,
    pub name: String,
    pub kind: String,
    pub position: String,
    pub config: Value,
}

/// What rows to read and in what order, by column names.
#[derive(Clone, Debug, Default)]
pub struct Query {
    /// Conditions every row keeps: `(column, operator, value)` — an operator
    /// the column's type takes ("equals", "contains", "is_empty",
    /// "greater_than", "before", …) and an option's name, a person's or row's
    /// ID, text, a number, true or false, a date.
    pub filters: Vec<(String, String, Value)>,
    /// `(column, descending)`; without any, rows keep the table's own order.
    pub sorts: Vec<(String, bool)>,
    /// Keeps the rows whose text contains it.
    pub search: String,
    /// Reads the archived rows instead of the others.
    pub archived: bool,
    /// How many rows a page of `find` holds, 1 to 100 (100 when 0).
    pub limit: i64,
}

/// A table's row as it was read.
#[derive(Clone, Debug)]
pub struct Row {
    pub id: String,
    pub position: String,
    pub created_at: String,
    pub created_by: String,
    pub updated_at: String,
    pub last_edited_by: String,
    values: Value,
}

fn text(value: Option<&Value>) -> String {
    value.and_then(Value::as_str).unwrap_or("").to_string()
}

fn call(tenancy: &str, method: &str, path: &str, query: &str, body: Value) -> Result<Value, String> {
    let mut request = vec![("method".to_string(), Value::from(method)), ("path".to_string(), Value::from(path))];
    for (key, given) in [("tenancy", tenancy), ("query", query)] {
        if !given.is_empty() {
            request.push((key.to_string(), given.into()));
        }
    }
    if body != Value::Null {
        request.push(("body".to_string(), body));
    }
    let answer = api(&Value::Object(request))?;
    let status = answer.get("status").and_then(Value::as_f64).unwrap_or(0.0) as i64;
    if status < 300 {
        return Ok(answer.get("body").cloned().unwrap_or(Value::Null));
    }
    let refusal = answer.get("body").and_then(|body| body.get("error"));
    let detail = match refusal.and_then(|error| error.get("code")).and_then(Value::as_str) {
        Some(code) => format!("{}: {}", code, text(refusal.and_then(|error| error.get("message")))),
        None => text(answer.get("text")),
    };
    Err(format!("{} {} {}: {}", status, method, path, detail))
}

impl Table {
    /// Finds the table its data setup step's key names, in the organization
    /// `tenancy` names (empty: the run's own).
    pub fn open(tenancy: &str, key: &str) -> Result<Table, String> {
        let data = call(tenancy, "GET", "/app-data", "", Value::Null)?;
        let items = data.get("items").and_then(Value::as_array).unwrap_or(&[]);
        for item in items {
            if text(item.get("key")) != key || text(item.get("dataSourceId")).is_empty() {
                continue;
            }
            let properties = item.get("properties").and_then(Value::as_array).unwrap_or(&[]);
            if text(item.get("pageId")).is_empty() || properties.is_empty() {
                return Err(format!("leafen: the {key:?} table can't be read here"));
            }
            let columns = properties.iter().map(|property| Column {
                id: text(property.get("id")),
                name: text(property.get("name")),
                kind: text(property.get("type")),
                position: text(property.get("position")),
                config: property.get("config").cloned().unwrap_or(Value::Object(Vec::new())),
            });
            return Ok(Table {
                tenancy: tenancy.to_string(),
                key: key.to_string(),
                page_id: text(item.get("pageId")),
                data_source_id: text(item.get("dataSourceId")),
                columns: columns.collect(),
            });
        }
        Err(format!("leafen: no {key:?} table is set up here yet"))
    }

    /// The column with this name.
    pub fn column(&self, name: &str) -> Option<&Column> {
        self.columns.iter().find(|column| column.name == name)
    }

    fn named(&self, name: &str) -> Result<&Column, String> {
        self.column(name).ok_or_else(|| format!("leafen: the {:?} table has no {name:?} column", self.key))
    }

    /// Reads a page of the rows the query keeps, from where `cursor` (empty
    /// at first) says, and the next page's cursor — empty after the last.
    /// Leafen works through a large table a part at a time, so a page may
    /// come back empty before the last one.
    pub fn find(&self, query: &Query, cursor: &str) -> Result<(Vec<Row>, String), String> {
        self.page(query, cursor, false).map(|(rows, next, _)| (rows, next))
    }

    /// How many rows the query keeps: about one host call for every five
    /// thousand rows Leafen walks when every condition names options, people,
    /// rows or a checked checkbox (and there is no search), otherwise as
    /// `find` reads them.
    pub fn count(&self, query: &Query) -> Result<i64, String> {
        let query = Query { limit: 1, ..query.clone() };
        let mut cursor = String::new();
        loop {
            let (_, next, total) = self.page(&query, &cursor, true)?;
            if let Some(total) = total {
                return Ok(total);
            }
            if next.is_empty() {
                return Err("leafen: the query ended without a count".to_string());
            }
            cursor = next;
        }
    }

    fn page(&self, query: &Query, cursor: &str, count_only: bool) -> Result<(Vec<Row>, String, Option<i64>), String> {
        let mut filters = Vec::new();
        for (column, operator, value) in &query.filters {
            let id = self.named(column)?.id.clone();
            filters.push(Value::object([("propertyId", id.into()), ("operator", operator.as_str().into()), ("value", value.clone())]));
        }
        let mut sorts = Vec::new();
        for (column, descending) in &query.sorts {
            let direction = if *descending { "descending" } else { "ascending" };
            sorts.push(Value::object([("propertyId", self.named(column)?.id.clone().into()), ("direction", direction.into())]));
        }
        let mut request = vec![
            ("dataSourceId".to_string(), Value::from(self.data_source_id.as_str())),
            ("limit".to_string(), Value::from(if query.limit == 0 { 100 } else { query.limit })),
            ("sorts".to_string(), Value::Array(sorts)),
            ("countOnly".to_string(), count_only.into()),
        ];
        if !filters.is_empty() {
            let group = Value::object([("conjunction", "and".into()), ("filters", filters.into()), ("groups", Value::Array(Vec::new()))]);
            request.push(("filterGroup".to_string(), group));
        }
        if query.archived {
            request.push(("archiveScope".to_string(), "archived".into()));
        }
        if !query.search.is_empty() {
            request.push(("search".to_string(), query.search.as_str().into()));
        }
        if !cursor.is_empty() {
            request.push(("cursor".to_string(), cursor.into()));
        }
        let path = format!("/pages/{}/database-query", self.page_id);
        let answer = call(&self.tenancy, "POST", &path, "", Value::Object(request))?;
        let mut rows = Vec::new();
        for item in answer.get("rows").and_then(Value::as_array).unwrap_or(&[]) {
            let mut values = match item.get("values") {
                Some(Value::Object(fields)) => fields.clone(),
                _ => Vec::new(),
            };
            if let Some(Value::Object(computed)) = item.get("computed") {
                values.extend(computed.iter().cloned());
            }
            rows.push(Row::read(item, Value::Object(values)));
        }
        let more = answer.get("hasMore").and_then(Value::as_bool).unwrap_or(false);
        let total = answer.get("total").and_then(Value::as_f64).map(|total| total as i64);
        Ok((rows, if more { text(answer.get("nextCursor")) } else { String::new() }, total))
    }

    /// Goes through every row the query keeps, to the last, until `visit`
    /// returns false. Each page is a host call, and a run has 200: Leafen
    /// walks a hundred rows of the table a call — five thousand when the
    /// conditions name options, people, rows or a checked checkbox — and
    /// gives back a hundred rows a call.
    pub fn each(&self, query: &Query, mut visit: impl FnMut(&Row) -> bool) -> Result<(), String> {
        let mut cursor = String::new();
        loop {
            let (rows, next) = self.find(query, &cursor)?;
            for row in &rows {
                if !visit(row) {
                    return Ok(());
                }
            }
            if next.is_empty() {
                return Ok(());
            }
            cursor = next;
        }
    }

    /// Reads one row by its ID, in one host call: its kept values — formulas
    /// and rollups come only with `find` and `each`. `None` when it isn't in
    /// the table.
    pub fn row(&self, id: &str) -> Result<Option<Row>, String> {
        let path = format!("/database-rows/{id}/page");
        let answer = match call(&self.tenancy, "GET", &path, "limit=1", Value::Null) {
            Err(error) if error.starts_with("404 ") => return Ok(None),
            other => other?,
        };
        let found = answer.get("row").cloned().unwrap_or(Value::Null);
        if text(found.get("dataSourceId")) != self.data_source_id || !text(found.get("trashedAt")).is_empty() {
            return Ok(None);
        }
        let values = found.get("values").cloned().unwrap_or(Value::Object(Vec::new()));
        Ok(Some(Row::read(&found, values)))
    }

    /// Makes a row after the ones the app added before, with values by
    /// column name (see `update`), and returns its ID.
    pub fn add(&self, values: &[(&str, Value)]) -> Result<String, String> {
        let encoded = self.encode(values)?;
        let id = random_hex();
        let row = Value::object([
            ("id", id.as_str().into()),
            ("dataSourceId", self.data_source_id.as_str().into()),
            ("position", format!("t{:013}", now()).into()),
            ("values", encoded),
        ]);
        let operation = Value::object([("kind", "upsertRow".into()), ("row", row)]);
        let body = Value::object([("mutationId", random_hex().into()), ("operations", vec![operation].into())]);
        call(&self.tenancy, "POST", &format!("/pages/{}/content-graph/mutations", self.page_id), "", body)?;
        Ok(id)
    }

    /// Changes the values it names, by column name, and leaves the rest of
    /// the row as it is. A value is what the column's type holds: text; a
    /// number; true or false; an option's name (an array of names for a
    /// multi-select); an array of people's or rows' IDs; a date as
    /// "2006-01-02", "start/end" or `{start, end}`; `Value::Null` clears it.
    /// An option the column doesn't have is refused, not dropped.
    pub fn update(&self, id: &str, values: &[(&str, Value)]) -> Result<(), String> {
        let body = Value::object([("mutationId", random_hex().into()), ("values", self.encode(values)?)]);
        call(&self.tenancy, "POST", &format!("/database-rows/{id}/page/values"), "", body).map(|_| ())
    }

    /// Changes many rows, `(id, values)` each as `update` takes them: a
    /// hundred rows to one host call and one write of the app's budget.
    pub fn update_many(&self, rows: &[(&str, Vec<(&str, Value)>)]) -> Result<(), String> {
        for chunk in rows.chunks(100) {
            let mut listed = Vec::new();
            for (id, values) in chunk {
                listed.push(Value::object([("rowId", (*id).into()), ("values", self.encode(values)?)]));
            }
            let body = Value::object([("mutationId", random_hex().into()), ("rows", listed.into())]);
            call(&self.tenancy, "POST", &format!("/pages/{}/database-row-values", self.page_id), "", body)?;
        }
        Ok(())
    }

    /// Moves a row to the trash, where people can restore it, in two host
    /// calls; a row already there stays.
    pub fn remove(&self, id: &str) -> Result<(), String> {
        let path = format!("/database-rows/{id}/page");
        let row = call(&self.tenancy, "GET", &path, "limit=1", Value::Null)?.get("row").cloned().unwrap_or(Value::Null);
        if text(row.get("dataSourceId")) != self.data_source_id {
            return Err(format!("leafen: the table has no row {id:?}"));
        }
        if !text(row.get("trashedAt")).is_empty() {
            return Ok(());
        }
        let revision = row.get("authorityRevision").and_then(Value::as_f64).unwrap_or(0.0);
        let body = Value::object([("requestId", random_hex().into()), ("expectedAuthorityRevision", revision.into())]);
        call(&self.tenancy, "POST", &format!("{path}/trash"), "", body).map(|_| ())
    }

    fn encode(&self, values: &[(&str, Value)]) -> Result<Value, String> {
        let mut encoded = Vec::new();
        for (name, value) in values {
            let column = self.named(name)?;
            let mut value = value.clone();
            if matches!(column.kind.as_str(), "select" | "status" | "multi_select") {
                value = option_ids(column, &value)?;
            }
            encoded.push((column.id.clone(), Value::object([("type", column.kind.as_str().into()), ("value", value)])));
        }
        Ok(Value::Object(encoded))
    }
}

impl Column {
    /// A select, multi-select or status column's options as `(id, name)`.
    pub fn options(&self) -> Vec<(String, String)> {
        let options = self.config.get("options").and_then(Value::as_array).unwrap_or(&[]);
        options.iter().map(|option| (text(option.get("id")), text(option.get("name")))).collect()
    }
}

fn option_ids(column: &Column, value: &Value) -> Result<Value, String> {
    let options = column.options();
    let find = |name: &str| {
        options.iter().find(|(_, option)| option == name).map(|(id, _)| Value::from(id.as_str()))
            .ok_or_else(|| format!("leafen: the {:?} column has no {name:?} option", column.name))
    };
    match value {
        Value::Null => Ok(Value::Null),
        Value::String(name) if name.is_empty() => Ok(Value::Null),
        Value::String(name) => find(name),
        Value::Array(names) => names.iter().map(|name| find(name.as_str().unwrap_or(""))).collect::<Result<Vec<_>, _>>().map(Value::Array),
        _ => Err(format!("leafen: the {:?} column takes option names", column.name)),
    }
}

impl Row {
    fn read(item: &Value, values: Value) -> Row {
        Row {
            id: text(item.get("id")),
            position: text(item.get("position")),
            created_at: text(item.get("createdAt")),
            created_by: text(item.get("createdBy")),
            updated_at: text(item.get("updatedAt")),
            last_edited_by: text(item.get("lastEditedBy")),
            values,
        }
    }

    /// A column's value as Leafen keeps it (`Value::Null` when empty).
    pub fn value(&self, table: &Table, column: &str) -> Value {
        let id = table.column(column).map(|found| found.id.as_str()).unwrap_or("");
        self.values.get(id).and_then(|cell| cell.get("value")).cloned().unwrap_or(Value::Null)
    }

    /// A text, title, URL, email or phone column's value, or a formula's or
    /// rollup's as text.
    pub fn text(&self, table: &Table, column: &str) -> String {
        let id = table.column(column).map(|found| found.id.as_str()).unwrap_or("");
        if let Some(shown) = self.values.get(id).and_then(|cell| cell.get("text")).and_then(Value::as_str) {
            return shown.to_string();
        }
        text(Some(&self.value(table, column)))
    }

    /// A number column's value (0 when empty).
    pub fn number(&self, table: &Table, column: &str) -> f64 {
        self.value(table, column).as_f64().unwrap_or(0.0)
    }

    /// A checkbox column's value.
    pub fn checkbox(&self, table: &Table, column: &str) -> bool {
        self.value(table, column).as_bool().unwrap_or(false)
    }

    /// A select or status column's option name ("" when empty).
    pub fn option(&self, table: &Table, column: &str) -> String {
        self.options(table, column).into_iter().next().unwrap_or_default()
    }

    /// A multi-select column's option names.
    pub fn options(&self, table: &Table, column: &str) -> Vec<String> {
        let names = table.column(column).map(Column::options).unwrap_or_default();
        let value = self.value(table, column);
        let ids: Vec<&str> = match &value {
            Value::String(id) => vec![id.as_str()],
            Value::Array(items) => items.iter().filter_map(Value::as_str).collect(),
            _ => Vec::new(),
        };
        ids.iter().filter_map(|id| names.iter().find(|(option, _)| option == id).map(|(_, name)| name.clone())).collect()
    }

    /// A person or relation column's people or rows.
    pub fn ids(&self, table: &Table, column: &str) -> Vec<String> {
        let value = self.value(table, column);
        value.as_array().unwrap_or(&[]).iter().filter_map(Value::as_str).map(str::to_string).collect()
    }

    /// A date column's start and end ("" when there is none).
    pub fn date(&self, table: &Table, column: &str) -> (String, String) {
        let value = self.value(table, column);
        (text(value.get("start")), text(value.get("end")))
    }
}
