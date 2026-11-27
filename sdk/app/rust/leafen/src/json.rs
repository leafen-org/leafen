//! JSON for what a worker reads and writes, small enough to need no crate.
//! Objects keep their keys in order; numbers are `f64`.

use std::fmt::Write;

/// A JSON value.
#[derive(Clone, Debug, PartialEq)]
pub enum Value {
    Null,
    Bool(bool),
    Number(f64),
    String(String),
    Array(Vec<Value>),
    Object(Vec<(String, Value)>),
}

const MAX_DEPTH: usize = 128;

impl Value {
    /// An object from its fields, in order.
    pub fn object<const N: usize>(fields: [(&str, Value); N]) -> Value {
        Value::Object(fields.into_iter().map(|(key, value)| (key.to_string(), value)).collect())
    }

    /// Reads one JSON value; anything after it but white space is an error.
    pub fn parse(text: &[u8]) -> Result<Value, String> {
        let mut reader = Reader { text, at: 0 };
        let value = reader.value(0)?;
        reader.space();
        if reader.at != text.len() {
            return Err(format!("unexpected text at byte {}", reader.at));
        }
        Ok(value)
    }

    /// The field `key` of an object.
    pub fn get(&self, key: &str) -> Option<&Value> {
        match self {
            Value::Object(fields) => fields.iter().find(|(name, _)| name == key).map(|(_, value)| value),
            _ => None,
        }
    }

    pub fn as_str(&self) -> Option<&str> {
        match self {
            Value::String(text) => Some(text),
            _ => None,
        }
    }

    pub fn as_f64(&self) -> Option<f64> {
        match self {
            Value::Number(number) => Some(*number),
            _ => None,
        }
    }

    pub fn as_bool(&self) -> Option<bool> {
        match self {
            Value::Bool(flag) => Some(*flag),
            _ => None,
        }
    }

    pub fn as_array(&self) -> Option<&[Value]> {
        match self {
            Value::Array(items) => Some(items),
            _ => None,
        }
    }

    /// The value as JSON text.
    pub fn to_json(&self) -> String {
        let mut out = String::new();
        self.write(&mut out);
        out
    }

    fn write(&self, out: &mut String) {
        match self {
            Value::Null => out.push_str("null"),
            Value::Bool(flag) => out.push_str(if *flag { "true" } else { "false" }),
            Value::Number(number) if !number.is_finite() => out.push_str("null"),
            Value::Number(number) if number.fract() == 0.0 && number.abs() < 1e15 => {
                let _ = write!(out, "{}", *number as i64);
            }
            Value::Number(number) => {
                let _ = write!(out, "{number}");
            }
            Value::String(text) => write_string(text, out),
            Value::Array(items) => {
                out.push('[');
                for (index, item) in items.iter().enumerate() {
                    if index > 0 {
                        out.push(',');
                    }
                    item.write(out);
                }
                out.push(']');
            }
            Value::Object(fields) => {
                out.push('{');
                for (index, (key, value)) in fields.iter().enumerate() {
                    if index > 0 {
                        out.push(',');
                    }
                    write_string(key, out);
                    out.push(':');
                    value.write(out);
                }
                out.push('}');
            }
        }
    }
}

fn write_string(text: &str, out: &mut String) {
    out.push('"');
    for character in text.chars() {
        match character {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            control if (control as u32) < 0x20 => {
                let _ = write!(out, "\\u{:04x}", control as u32);
            }
            other => out.push(other),
        }
    }
    out.push('"');
}

impl From<&str> for Value {
    fn from(text: &str) -> Value {
        Value::String(text.to_string())
    }
}

impl From<String> for Value {
    fn from(text: String) -> Value {
        Value::String(text)
    }
}

impl From<f64> for Value {
    fn from(number: f64) -> Value {
        Value::Number(number)
    }
}

impl From<i64> for Value {
    fn from(number: i64) -> Value {
        Value::Number(number as f64)
    }
}

impl From<bool> for Value {
    fn from(flag: bool) -> Value {
        Value::Bool(flag)
    }
}

impl From<Vec<Value>> for Value {
    fn from(items: Vec<Value>) -> Value {
        Value::Array(items)
    }
}

struct Reader<'a> {
    text: &'a [u8],
    at: usize,
}

impl Reader<'_> {
    fn space(&mut self) {
        while matches!(self.text.get(self.at), Some(b' ' | b'\n' | b'\r' | b'\t')) {
            self.at += 1;
        }
    }

    fn fail<T>(&self, what: &str) -> Result<T, String> {
        Err(format!("{what} at byte {}", self.at))
    }

    fn literal(&mut self, word: &str, value: Value) -> Result<Value, String> {
        if self.text[self.at..].starts_with(word.as_bytes()) {
            self.at += word.len();
            Ok(value)
        } else {
            self.fail("unexpected text")
        }
    }

    fn value(&mut self, depth: usize) -> Result<Value, String> {
        if depth > MAX_DEPTH {
            return self.fail("too deep");
        }
        self.space();
        match self.text.get(self.at) {
            Some(b'n') => self.literal("null", Value::Null),
            Some(b't') => self.literal("true", Value::Bool(true)),
            Some(b'f') => self.literal("false", Value::Bool(false)),
            Some(b'"') => Ok(Value::String(self.string()?)),
            Some(b'[') => {
                self.at += 1;
                let mut items = Vec::new();
                self.space();
                if self.text.get(self.at) == Some(&b']') {
                    self.at += 1;
                    return Ok(Value::Array(items));
                }
                loop {
                    items.push(self.value(depth + 1)?);
                    self.space();
                    match self.text.get(self.at) {
                        Some(b',') => self.at += 1,
                        Some(b']') => {
                            self.at += 1;
                            return Ok(Value::Array(items));
                        }
                        _ => return self.fail("expected , or ]"),
                    }
                }
            }
            Some(b'{') => {
                self.at += 1;
                let mut fields = Vec::new();
                self.space();
                if self.text.get(self.at) == Some(&b'}') {
                    self.at += 1;
                    return Ok(Value::Object(fields));
                }
                loop {
                    self.space();
                    if self.text.get(self.at) != Some(&b'"') {
                        return self.fail("expected a key");
                    }
                    let key = self.string()?;
                    self.space();
                    if self.text.get(self.at) != Some(&b':') {
                        return self.fail("expected :");
                    }
                    self.at += 1;
                    fields.push((key, self.value(depth + 1)?));
                    self.space();
                    match self.text.get(self.at) {
                        Some(b',') => self.at += 1,
                        Some(b'}') => {
                            self.at += 1;
                            return Ok(Value::Object(fields));
                        }
                        _ => return self.fail("expected , or }"),
                    }
                }
            }
            Some(b'-' | b'0'..=b'9') => self.number(),
            _ => self.fail("expected a value"),
        }
    }

    fn number(&mut self) -> Result<Value, String> {
        let start = self.at;
        while matches!(self.text.get(self.at), Some(b'-' | b'+' | b'.' | b'e' | b'E' | b'0'..=b'9')) {
            self.at += 1;
        }
        std::str::from_utf8(&self.text[start..self.at])
            .ok()
            .and_then(|text| text.parse::<f64>().ok())
            .map(Value::Number)
            .ok_or_else(|| format!("not a number at byte {start}"))
    }

    fn hex4(&mut self) -> Result<u32, String> {
        let digits = self.text.get(self.at..self.at + 4).and_then(|digits| std::str::from_utf8(digits).ok());
        match digits.and_then(|digits| u32::from_str_radix(digits, 16).ok()) {
            Some(code) => {
                self.at += 4;
                Ok(code)
            }
            None => self.fail("bad \\u escape"),
        }
    }

    fn string(&mut self) -> Result<String, String> {
        self.at += 1;
        let mut bytes = Vec::new();
        loop {
            match self.text.get(self.at) {
                None => return self.fail("unfinished string"),
                Some(b'"') => {
                    self.at += 1;
                    return String::from_utf8(bytes).map_err(|_| "a string isn't UTF-8".to_string());
                }
                Some(b'\\') => {
                    self.at += 1;
                    let escaped = self.text.get(self.at).copied();
                    self.at += 1;
                    let character = match escaped {
                        Some(b'"') => '"',
                        Some(b'\\') => '\\',
                        Some(b'/') => '/',
                        Some(b'b') => '\u{8}',
                        Some(b'f') => '\u{c}',
                        Some(b'n') => '\n',
                        Some(b'r') => '\r',
                        Some(b't') => '\t',
                        Some(b'u') => {
                            let mut code = self.hex4()?;
                            if (0xd800..0xdc00).contains(&code) && self.text[self.at..].starts_with(b"\\u") {
                                self.at += 2;
                                let low = self.hex4()?;
                                code = 0x10000 + ((code - 0xd800) << 10) + (low.wrapping_sub(0xdc00) & 0x3ff);
                            }
                            char::from_u32(code).unwrap_or('\u{fffd}')
                        }
                        _ => return self.fail("bad escape"),
                    };
                    let mut buffer = [0u8; 4];
                    bytes.extend_from_slice(character.encode_utf8(&mut buffer).as_bytes());
                }
                Some(&byte) if byte < 0x20 => return self.fail("a control character in a string"),
                Some(&byte) => {
                    bytes.push(byte);
                    self.at += 1;
                }
            }
        }
    }
}

