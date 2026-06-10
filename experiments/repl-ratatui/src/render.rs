use crate::app::ResultBlock;
use crate::diff::{align_diff, can_split, DiffKind};
use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::{Color, Modifier, Style};
use ratatui::text::{Line, Span};
use ratatui::widgets::{Clear, Widget};
use unicode_width::UnicodeWidthStr;

const MIN_SPLIT_PANE_WIDTH: usize = 50;
const SPLIT_GAP_WIDTH: usize = 4;
const CURSOR_MARKER: &str = "\u{e000}";
const STARTER_PROMPT: &str = "[ '' ] ( '' )";
const STARTER_TARGET_CURSOR: usize = 3;
const STARTER_COMMAND_CURSOR: usize = 10;

#[derive(Default)]
pub struct TranscriptCache {
    key: Option<TranscriptKey>,
    lines: Vec<Line<'static>>,
    render_count: usize,
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct TranscriptKey {
    width: usize,
    diff_enabled: bool,
    results: Vec<ResultKey>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct ResultKey {
    batch: usize,
    index: usize,
    host_len: usize,
    command_len: usize,
    output_ptr: usize,
    output_len: usize,
    error_len: usize,
}

impl TranscriptCache {
    pub fn render(&mut self, results: &[ResultBlock], width: usize) -> &[Line<'static>] {
        self.render_with_diff(results, width, false)
    }

    pub fn render_with_diff(
        &mut self,
        results: &[ResultBlock],
        width: usize,
        diff_enabled: bool,
    ) -> &[Line<'static>] {
        let key = TranscriptKey::new(results, width, diff_enabled);
        if self.key.as_ref() != Some(&key) {
            self.lines = transcript_lines(results, width, diff_enabled);
            self.key = Some(key);
            self.render_count += 1;
        }
        &self.lines
    }

    pub fn render_count(&self) -> usize {
        self.render_count
    }
}

pub fn visible_lines(lines: &[Line<'static>], scroll: usize, height: usize) -> Vec<Line<'static>> {
    if height == 0 || lines.is_empty() {
        return Vec::new();
    }
    let max_start = lines.len().saturating_sub(height);
    let start = scroll.min(max_start);
    let end = (start + height).min(lines.len());
    lines[start..end].to_vec()
}

pub fn max_scroll(line_count: usize, height: usize) -> usize {
    line_count.saturating_sub(height)
}

pub struct TranscriptView {
    lines: Vec<Line<'static>>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PromptCursor {
    Pipe,
    Underscore,
}

impl Default for PromptCursor {
    fn default() -> Self {
        PromptCursor::Pipe
    }
}

pub fn transcript_view(lines: Vec<Line<'static>>) -> TranscriptView {
    TranscriptView { lines }
}

pub fn prompt_line(prompt: &str, suggestion: &str) -> Line<'static> {
    prompt_line_internal(prompt, suggestion, None)
}

pub fn prompt_line_with_cursor(
    prompt: &str,
    suggestion: &str,
    _cursor: PromptCursor,
) -> Line<'static> {
    prompt_line_with_cursor_at(prompt, suggestion, _cursor, prompt.chars().count())
}

pub fn prompt_line_with_cursor_at(
    prompt: &str,
    suggestion: &str,
    _cursor: PromptCursor,
    _cursor_pos: usize,
) -> Line<'static> {
    prompt_line_internal(prompt, suggestion, None)
}

pub fn prompt_cursor_column(prompt: &str, _suggestion: &str, cursor_pos: usize) -> usize {
    if prompt == STARTER_PROMPT {
        if cursor_pos == STARTER_TARGET_CURSOR {
            return UnicodeWidthStr::width("nssh> ['");
        }
        if cursor_pos == STARTER_COMMAND_CURSOR {
            return UnicodeWidthStr::width("nssh> ['TARGET'] ('");
        }
    }
    let byte = byte_index_for_char(prompt, cursor_pos);
    let text = format!("nssh> {}", &prompt[..byte]);
    UnicodeWidthStr::width(text.as_str())
}

fn prompt_line_internal(prompt: &str, suggestion: &str, cursor: Option<usize>) -> Line<'static> {
    let mut spans = vec![Span::styled("nssh> ", Style::default().fg(Color::Gray))];
    spans.extend(raw_prompt_spans(prompt, suggestion, cursor));
    Line::from(spans)
}

fn raw_prompt_spans(prompt: &str, suggestion: &str, cursor: Option<usize>) -> Vec<Span<'static>> {
    if prompt == STARTER_PROMPT && suggestion.trim_end().is_empty() {
        return starter_prompt_spans(cursor);
    }
    let mut spans = Vec::new();
    let mut mode = raw_prompt_mode(prompt);
    let groups = PromptGroupState::new(prompt, suggestion);
    let suggestion_pos = if suggestion.trim_end().is_empty() {
        None
    } else {
        active_target_end(prompt)
    };
    for (idx, ch) in prompt.chars().enumerate() {
        if cursor == Some(idx) {
            spans.push(cursor_marker_span());
        }
        let style = prompt_style_for_char(prompt, idx, ch, mode, groups);
        spans.push(Span::styled(ch.to_string(), style));
        if Some(idx + 1) == suggestion_pos {
            spans.push(Span::styled(
                suggestion.trim_end().to_string(),
                suggestion_style(),
            ));
        }
        mode.advance(ch);
    }
    if cursor == Some(prompt.chars().count()) {
        spans.push(cursor_marker_span());
    }
    let suggestion = suggestion.trim_end();
    if !suggestion.is_empty() && suggestion_pos.is_none() {
        spans.push(Span::styled(suggestion.to_string(), suggestion_style()));
    }
    spans
}

fn starter_prompt_spans(cursor: Option<usize>) -> Vec<Span<'static>> {
    let mut spans = Vec::new();
    spans.push(Span::styled("[", prompt_group_style()));
    spans.push(Span::styled("'", prompt_group_style()));
    if cursor == Some(STARTER_TARGET_CURSOR) {
        spans.push(cursor_marker_span());
    }
    spans.push(Span::styled("TARGET", placeholder_target_style()));
    spans.push(Span::styled("'", prompt_group_style()));
    spans.push(Span::styled("] ", prompt_group_style()));
    spans.push(Span::styled("(", prompt_group_style()));
    spans.push(Span::styled("'", prompt_group_style()));
    if cursor == Some(STARTER_COMMAND_CURSOR) {
        spans.push(cursor_marker_span());
    }
    spans.push(Span::styled("COMMAND", placeholder_command_style()));
    spans.push(Span::styled("'", prompt_group_style()));
    spans.push(Span::styled(")", prompt_group_style()));
    spans
}

fn active_target_end(prompt: &str) -> Option<usize> {
    let close = prompt.find(']')?;
    if prompt[close + 1..].trim() != "( '' )" {
        return None;
    }
    let body = &prompt[1..close];
    let trimmed = body.trim();
    if !trimmed.starts_with('\'') || !trimmed.ends_with('\'') || trimmed.matches('\'').count() != 2
    {
        return None;
    }
    Some(1 + body.find(trimmed)? + trimmed.chars().count() - 1)
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum PromptRegion {
    Group,
    Target,
    Command,
}

#[derive(Clone, Copy)]
struct RawPromptMode {
    region: PromptRegion,
    in_quote: bool,
    escaped: bool,
}

#[derive(Clone, Copy)]
struct PromptGroupState {
    target_filled: bool,
    command_filled: bool,
}

impl PromptGroupState {
    fn new(prompt: &str, suggestion: &str) -> Self {
        Self {
            target_filled: target_body(prompt)
                .map(quoted_group_has_content)
                .unwrap_or(false)
                && suggestion.trim_end().is_empty(),
            command_filled: command_body(prompt)
                .map(quoted_group_has_content)
                .unwrap_or(false),
        }
    }
}

fn raw_prompt_mode(_prompt: &str) -> RawPromptMode {
    RawPromptMode {
        region: PromptRegion::Group,
        in_quote: false,
        escaped: false,
    }
}

impl RawPromptMode {
    fn advance(&mut self, ch: char) {
        if self.escaped {
            self.escaped = false;
            return;
        }
        if self.in_quote && ch == '\\' {
            self.escaped = true;
            return;
        }
        if ch == '\'' {
            self.in_quote = !self.in_quote;
            return;
        }
        if self.in_quote {
            return;
        }
        match ch {
            '[' => self.region = PromptRegion::Target,
            ']' => self.region = PromptRegion::Group,
            '(' => self.region = PromptRegion::Command,
            ')' => self.region = PromptRegion::Group,
            _ => {}
        }
    }
}

fn prompt_style_for_char(
    prompt: &str,
    idx: usize,
    ch: char,
    mode: RawPromptMode,
    groups: PromptGroupState,
) -> Style {
    match mode.region {
        PromptRegion::Target if mode.in_quote && ch != '\'' => target_style(),
        PromptRegion::Command if mode.in_quote && ch != '\'' => {
            command_rune_style_for_text(prompt, idx)
        }
        _ if ch == '[' || ch == ']' || mode.region == PromptRegion::Target => {
            prompt_group_style_for(groups.target_filled)
        }
        _ if ch == '(' || ch == ')' || mode.region == PromptRegion::Command => {
            prompt_group_style_for(groups.command_filled)
        }
        _ => prompt_group_style(),
    }
}

fn target_body(prompt: &str) -> Option<&str> {
    let open = prompt.find('[')?;
    let close = matching_group_end(prompt, open, '[', ']')?;
    Some(&prompt[open + 1..close])
}

fn command_body(prompt: &str) -> Option<&str> {
    let open = prompt.find('(')?;
    let close = matching_group_end(prompt, open, '(', ')')?;
    Some(&prompt[open + 1..close])
}

fn matching_group_end(prompt: &str, open: usize, _start: char, end: char) -> Option<usize> {
    let mut in_quote = false;
    let mut escaped = false;
    for (offset, ch) in prompt[open + 1..].char_indices() {
        let idx = open + 1 + offset;
        if escaped {
            escaped = false;
            continue;
        }
        if in_quote && ch == '\\' {
            escaped = true;
            continue;
        }
        if ch == '\'' {
            in_quote = !in_quote;
            continue;
        }
        if !in_quote && ch == end {
            return Some(idx);
        }
    }
    None
}

fn quoted_group_has_content(body: &str) -> bool {
    let mut in_quote = false;
    let mut escaped = false;
    let mut current = String::new();
    for ch in body.chars() {
        if escaped {
            if in_quote {
                current.push(ch);
            }
            escaped = false;
            continue;
        }
        if in_quote && ch == '\\' {
            escaped = true;
            continue;
        }
        if ch == '\'' {
            if in_quote && !current.trim().is_empty() {
                return true;
            }
            if in_quote {
                current.clear();
            }
            in_quote = !in_quote;
            continue;
        }
        if in_quote {
            current.push(ch);
        }
    }
    false
}

fn command_rune_style_for_text(prompt: &str, idx: usize) -> Style {
    let chars: Vec<char> = prompt.chars().collect();
    let mut start = idx;
    while start > 0
        && !chars[start - 1].is_whitespace()
        && chars[start - 1] != '\''
        && chars[start - 1] != ','
    {
        start -= 1;
    }
    if chars.get(start) == Some(&'-') {
        flag_style()
    } else {
        command_style()
    }
}

fn byte_index_for_char(value: &str, char_index: usize) -> usize {
    value
        .char_indices()
        .nth(char_index)
        .map(|(index, _)| index)
        .unwrap_or(value.len())
}

fn cursor_marker_span() -> Span<'static> {
    Span::raw(CURSOR_MARKER)
}

fn prompt_group_style() -> Style {
    Style::default().fg(Color::Gray).add_modifier(Modifier::DIM)
}

fn prompt_group_solid_style() -> Style {
    Style::default().fg(Color::Gray)
}

fn prompt_group_style_for(filled: bool) -> Style {
    if filled {
        prompt_group_solid_style()
    } else {
        prompt_group_style()
    }
}

fn target_style() -> Style {
    Style::default()
        .fg(Color::Cyan)
        .add_modifier(Modifier::BOLD)
}

fn placeholder_target_style() -> Style {
    target_style().add_modifier(Modifier::DIM)
}

fn placeholder_command_style() -> Style {
    command_style().add_modifier(Modifier::DIM)
}

fn command_style() -> Style {
    Style::reset().fg(Color::LightGreen)
}

fn flag_style() -> Style {
    Style::default().fg(Color::Yellow)
}

fn suggestion_style() -> Style {
    Style::default().fg(Color::DarkGray)
}

impl Widget for TranscriptView {
    fn render(self, area: Rect, buf: &mut Buffer) {
        Clear.render(area, buf);
        for (offset, line) in self.lines.iter().take(area.height as usize).enumerate() {
            buf.set_line(area.left(), area.top() + offset as u16, line, area.width);
        }
    }
}

impl TranscriptKey {
    fn new(results: &[ResultBlock], width: usize, diff_enabled: bool) -> Self {
        Self {
            width,
            diff_enabled,
            results: results.iter().map(ResultKey::new).collect(),
        }
    }
}

impl ResultKey {
    fn new(result: &ResultBlock) -> Self {
        Self {
            batch: result.batch,
            index: result.index,
            host_len: result.host.len(),
            command_len: result.command.len(),
            output_ptr: result.output.as_ptr() as usize,
            output_len: result.output.len(),
            error_len: result.error.len(),
        }
    }
}

fn transcript_lines(
    results: &[ResultBlock],
    width: usize,
    diff_enabled: bool,
) -> Vec<Line<'static>> {
    let mut lines = Vec::new();
    for batch in result_batches(results) {
        if batch.len() == 2 && can_split(width, MIN_SPLIT_PANE_WIDTH, SPLIT_GAP_WIDTH) {
            lines.extend(split_lines(&batch[0], &batch[1], width, diff_enabled));
            continue;
        }
        for result in batch {
            lines.push(Line::from(Span::styled(
                format!("--- {} | {} ---", result.host, result.command),
                Style::default().fg(Color::Cyan),
            )));
            for line in result.output.lines() {
                lines.push(Line::from(line.to_string()));
            }
            if !result.error.is_empty() {
                lines.push(Line::from(Span::styled(
                    format!("error: {}", result.error),
                    Style::default().fg(Color::Red),
                )));
            }
        }
    }
    lines
}

fn result_batches(results: &[ResultBlock]) -> Vec<Vec<&ResultBlock>> {
    let mut batches: Vec<Vec<&ResultBlock>> = Vec::new();
    for result in results {
        if batches
            .last()
            .and_then(|batch| batch.first())
            .is_some_and(|first| first.batch == result.batch)
        {
            batches.last_mut().expect("last batch exists").push(result);
        } else {
            batches.push(vec![result]);
        }
    }
    batches
}

fn split_lines(
    left: &ResultBlock,
    right: &ResultBlock,
    width: usize,
    diff_enabled: bool,
) -> Vec<Line<'static>> {
    let pane_width = (width - SPLIT_GAP_WIDTH) / 2;
    let body_width = pane_width.saturating_sub(5);
    let left_lines: Vec<String> = left.output.lines().map(str::to_string).collect();
    let right_lines: Vec<String> = right.output.lines().map(str::to_string).collect();
    let mut lines = vec![Line::from(vec![
        banner(&left.host, &left.command, pane_width),
        Span::raw(" ".repeat(SPLIT_GAP_WIDTH)),
        banner(&right.host, &right.command, pane_width),
    ])];

    for row in split_rows(&left_lines, &right_lines, diff_enabled) {
        let left_wrapped = wrap_side(row.left_line, &row.left_text, body_width);
        let right_wrapped = wrap_side(row.right_line, &row.right_text, body_width);
        let visual_rows = left_wrapped.len().max(right_wrapped.len()).max(1);
        for index in 0..visual_rows {
            let left_segment = left_wrapped.get(index).cloned().unwrap_or_default();
            let right_segment = right_wrapped.get(index).cloned().unwrap_or_default();
            let mut spans = vec![gutter_span(left_segment.gutter)];
            spans.extend(body_spans(
                left_segment.body,
                row.left_kind,
                body_width,
                true,
                diff_enabled,
            ));
            spans.push(Span::raw(" ".repeat(SPLIT_GAP_WIDTH)));
            spans.push(gutter_span(right_segment.gutter));
            spans.extend(body_spans(
                right_segment.body,
                row.right_kind,
                body_width,
                false,
                diff_enabled,
            ));
            lines.push(Line::from(spans));
        }
    }
    lines
}

fn split_rows(left: &[String], right: &[String], diff_enabled: bool) -> Vec<crate::diff::DiffRow> {
    if diff_enabled {
        return align_diff(left, right);
    }
    let height = left.len().max(right.len());
    (0..height)
        .map(|index| crate::diff::DiffRow {
            left_line: (index < left.len()).then_some(index + 1),
            left_kind: DiffKind::Equal,
            left_text: left.get(index).cloned().unwrap_or_default(),
            right_line: (index < right.len()).then_some(index + 1),
            right_kind: DiffKind::Equal,
            right_text: right.get(index).cloned().unwrap_or_default(),
        })
        .collect()
}

fn banner(host: &str, command: &str, width: usize) -> Span<'static> {
    let text = format!("--- {} | {} ", host, command);
    Span::styled(pad_to(text, width), Style::default().fg(Color::Cyan))
}

#[derive(Clone, Default)]
struct SideSegment {
    gutter: String,
    body: String,
}

fn wrap_side(line_number: Option<usize>, text: &str, body_width: usize) -> Vec<SideSegment> {
    let number = line_number.unwrap_or_default();
    let wrapped = crate::diff::wrap_body(number, text, body_width);
    wrapped
        .into_iter()
        .map(|row| match row.line_number {
            Some(value) if line_number.is_some() => SideSegment {
                gutter: format!("{value:>4} "),
                body: row.text,
            },
            _ => SideSegment {
                gutter: "     ".to_string(),
                body: row.text,
            },
        })
        .collect()
}

fn gutter_span(text: String) -> Span<'static> {
    Span::styled(
        text,
        Style::reset().fg(Color::Gray).add_modifier(Modifier::DIM),
    )
}

fn body_spans(
    text: String,
    kind: DiffKind,
    width: usize,
    left: bool,
    diff_enabled: bool,
) -> Vec<Span<'static>> {
    let bg = if diff_enabled {
        match (kind, left) {
            (DiffKind::LeftOnly | DiffKind::Changed, true) => Some(Color::Rgb(90, 24, 28)),
            (DiffKind::RightOnly | DiffKind::Changed, false) => Some(Color::Rgb(18, 74, 32)),
            _ => None,
        }
    } else {
        None
    };
    let text_width = text.chars().count();
    let padding_width = width.saturating_sub(text_width);
    let leading_width = text.chars().take_while(|ch| ch.is_whitespace()).count();
    let leading: String = text.chars().take(leading_width).collect();
    let body: String = text.chars().skip(leading_width).collect();
    let padding = " ".repeat(padding_width);

    if bg.is_none() {
        return vec![Span::styled(
            format!("{leading}{body}{padding}"),
            Style::reset(),
        )];
    }

    let mut spans = Vec::new();
    if !leading.is_empty() {
        spans.push(Span::styled(leading, Style::reset()));
    }
    if !body.is_empty() {
        spans.push(Span::styled(
            format!("{body}{padding}"),
            Style::reset().bg(bg.expect("diff background exists")),
        ));
    } else if !padding.is_empty() {
        spans.push(Span::styled(padding, Style::reset()));
    }
    spans
}

fn pad_to(mut text: String, width: usize) -> String {
    let current = text.chars().count();
    if current < width {
        text.push_str(&" ".repeat(width - current));
    } else if current > width {
        text = text.chars().take(width).collect();
    }
    text
}
