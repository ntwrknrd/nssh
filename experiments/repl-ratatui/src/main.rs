use std::io::{BufRead, BufReader, Write};
use std::path::{Path, PathBuf};
use std::process::{ChildStdin, Command, Stdio};
use std::sync::mpsc::{self, Receiver};
use std::time::Duration;

use anyhow::{Context, Result};
use crossterm::cursor::SetCursorStyle;
use crossterm::event::{
    self, DisableMouseCapture, EnableMouseCapture, Event, KeyCode, KeyEventKind, KeyModifiers,
};
use crossterm::execute;
use crossterm::terminal::{
    disable_raw_mode, enable_raw_mode, EnterAlternateScreen, LeaveAlternateScreen,
};
use nssh_repl_ratatui::app::App;
use nssh_repl_ratatui::completion::{
    active_target_prefix, complete_prompt, complete_selected_prompts, inline_suggestion,
    selected_prompt_preview, structural_complete_prompt, CompletionPicker,
};
use nssh_repl_ratatui::input::{apply_scroll_delta_clamped, drain_terminal_events, scroll_delta};
use nssh_repl_ratatui::protocol::{BrokerEvent, BrokerRequest};
use nssh_repl_ratatui::render::{
    max_scroll, prompt_cursor_column, prompt_line, transcript_view, visible_lines, PromptCursor,
    TranscriptCache,
};
use ratatui::backend::CrosstermBackend;
use ratatui::layout::{Constraint, Direction, Layout, Position};
use ratatui::style::{Color, Style};
use ratatui::text::{Line, Span};
use ratatui::widgets::{Block, Borders, Paragraph};
use ratatui::{Frame, Terminal};

fn main() -> Result<()> {
    let options = options_from_args(std::env::args());
    if options.help {
        println!("usage: nssh-repl-ratatui");
        println!();
        println!("Experimental Ratatui frontend for `nssh repl broker --json`.");
        println!();
        println!("Options:");
        println!("  --diff     highlight side-by-side output differences");
        println!("  --cursor   prompt cursor style: pipe or underscore");
        return Ok(());
    }

    let mut child = broker_command()
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .spawn()
        .context("spawn nssh repl broker --json")?;

    let mut broker_in = child.stdin.take().context("broker stdin")?;
    let broker_out = child.stdout.take().context("broker stdout")?;
    let events = spawn_event_reader(broker_out);

    send(&mut broker_in, &BrokerRequest::HistoryLoad)?;

    enable_raw_mode()?;
    let mut stdout = std::io::stdout();
    execute!(
        stdout,
        EnterAlternateScreen,
        EnableMouseCapture,
        terminal_cursor_style(options.cursor)
    )?;
    let backend = CrosstermBackend::new(stdout);
    let mut terminal = Terminal::new(backend)?;

    let result = run_ui(&mut terminal, &events, &mut broker_in, options);

    disable_raw_mode()?;
    execute!(
        terminal.backend_mut(),
        SetCursorStyle::DefaultUserShape,
        DisableMouseCapture,
        LeaveAlternateScreen
    )?;
    terminal.show_cursor()?;

    let _ = send(&mut broker_in, &BrokerRequest::Exit);
    let _ = child.wait();
    result
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
struct UiOptions {
    diff: bool,
    help: bool,
    cursor: PromptCursor,
}

fn options_from_args<I, S>(args: I) -> UiOptions
where
    I: IntoIterator<Item = S>,
    S: AsRef<str>,
{
    let mut options = UiOptions {
        cursor: PromptCursor::Pipe,
        ..UiOptions::default()
    };
    let mut args = args.into_iter().skip(1).peekable();
    while let Some(arg) = args.next() {
        let arg = arg.as_ref();
        match arg {
            "--diff" => options.diff = true,
            "--cursor=underscore" => options.cursor = PromptCursor::Underscore,
            "--cursor=pipe" => options.cursor = PromptCursor::Pipe,
            "--cursor" => {
                if let Some(value) = args.next() {
                    match value.as_ref() {
                        "underscore" => options.cursor = PromptCursor::Underscore,
                        "pipe" => options.cursor = PromptCursor::Pipe,
                        _ => {}
                    }
                }
            }
            "-h" | "--help" => options.help = true,
            _ => {}
        }
    }
    options
}

fn initial_prompt() -> String {
    "[ '' ] ( '' )".to_string()
}

fn initial_prompt_cursor() -> usize {
    3
}

fn command_prompt_cursor(prompt: &str) -> usize {
    prompt
        .find("( '' )")
        .map(|index| prompt[..index].chars().count() + 3)
        .unwrap_or_else(|| prompt_len(prompt))
}

fn terminal_cursor_style(cursor: PromptCursor) -> SetCursorStyle {
    match cursor {
        PromptCursor::Pipe => SetCursorStyle::SteadyBar,
        PromptCursor::Underscore => SetCursorStyle::SteadyUnderScore,
    }
}

fn broker_command() -> Command {
    let repo_root = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .parent()
        .and_then(Path::parent)
        .map(Path::to_path_buf);
    if let Some(repo_root) = repo_root {
        if repo_root.join("go.mod").exists() && repo_root.join("cmd/nssh").exists() {
            let mut command = Command::new("go");
            command
                .arg("run")
                .arg("./cmd/nssh")
                .args(["repl", "broker", "--json"])
                .current_dir(repo_root);
            return command;
        }
    }

    let mut command = Command::new("nssh");
    command.args(["repl", "broker", "--json"]);
    command
}

fn spawn_event_reader(out: impl std::io::Read + Send + 'static) -> Receiver<BrokerEvent> {
    let (tx, rx) = mpsc::channel();
    std::thread::spawn(move || {
        let reader = BufReader::new(out);
        for line in reader.lines().map_while(Result::ok) {
            if let Ok(event) = serde_json::from_str::<BrokerEvent>(&line) {
                if tx.send(event).is_err() {
                    break;
                }
            }
        }
    });
    rx
}

fn run_ui(
    terminal: &mut Terminal<CrosstermBackend<std::io::Stdout>>,
    events: &Receiver<BrokerEvent>,
    broker_in: &mut ChildStdin,
    options: UiOptions,
) -> Result<()> {
    let mut app = App::default();
    let mut transcript_cache = TranscriptCache::default();
    let mut prompt = initial_prompt();
    let mut prompt_cursor = initial_prompt_cursor();
    let mut scroll = 0usize;
    let mut history_index: Option<usize> = None;
    let mut picker = CompletionPicker::default();
    let mut dirty = true;

    'ui: loop {
        while let Ok(event) = events.try_recv() {
            if let BrokerEvent::Suggestions { suggestions } = &event {
                if suggestions.is_empty() {
                    picker.close();
                } else {
                    picker.clamp(suggestions.len());
                }
            }
            app.apply(event);
            dirty = true;
        }

        if dirty {
            app.set_prompt(prompt.clone());
            terminal.draw(|frame| {
                draw(
                    frame,
                    &app,
                    &picker,
                    &mut transcript_cache,
                    scroll,
                    options,
                    prompt_cursor,
                )
            })?;
            dirty = false;
        }

        if !event::poll(Duration::from_millis(40))? {
            continue;
        }
        let terminal_events = drain_terminal_events(event::read()?)?;
        let delta = scroll_delta(&terminal_events);
        if delta.net() != 0 {
            let max_scroll =
                current_max_scroll(terminal, &app, &picker, &mut transcript_cache, options)?;
            let next_scroll = apply_scroll_delta_clamped(scroll, delta, max_scroll);
            if next_scroll != scroll {
                scroll = next_scroll;
                dirty = true;
            }
        }
        for terminal_event in terminal_events {
            match terminal_event {
                Event::Key(key) if key.kind == KeyEventKind::Press => match key.code {
                    KeyCode::Char('c') if key.modifiers.contains(KeyModifiers::CONTROL) => {
                        break 'ui
                    }
                    KeyCode::Esc if picker.open => {
                        picker.close();
                        dirty = true;
                    }
                    KeyCode::Esc => break 'ui,
                    KeyCode::Enter => {
                        if picker.open {
                            if let Some(value) = complete_selected_prompts(
                                &prompt,
                                app.suggestions(),
                                picker.checked_indices(),
                            )
                            .or_else(|| {
                                selected_completion(&app, &picker)
                                    .and_then(|suggestion| complete_prompt(&prompt, suggestion))
                            }) {
                                prompt = value;
                                prompt_cursor = command_prompt_cursor(&prompt);
                                picker.close();
                                dirty = true;
                                continue;
                            }
                        }
                        let line = prompt.trim().to_string();
                        if !line.is_empty() && line != initial_prompt() {
                            send(
                                broker_in,
                                &BrokerRequest::HistoryAppend { line: line.clone() },
                            )?;
                            send(broker_in, &BrokerRequest::Submit { line })?;
                            prompt = initial_prompt();
                            prompt_cursor = initial_prompt_cursor();
                            history_index = None;
                            dirty = true;
                        }
                    }
                    KeyCode::Backspace => {
                        backspace_prompt_char(&mut prompt, &mut prompt_cursor);
                        picker.close();
                        suggest_if_target(broker_in, &prompt)?;
                        dirty = true;
                    }
                    KeyCode::Delete => {
                        delete_prompt_char(&mut prompt, &mut prompt_cursor);
                        picker.close();
                        suggest_if_target(broker_in, &prompt)?;
                        dirty = true;
                    }
                    KeyCode::Char(ch) => {
                        if picker.open && ch == ' ' {
                            picker.toggle_selected(app.suggestions().len());
                            dirty = true;
                            continue;
                        }
                        insert_prompt_char(&mut prompt, &mut prompt_cursor, ch);
                        picker.close();
                        suggest_if_target(broker_in, &prompt)?;
                        dirty = true;
                    }
                    KeyCode::Tab => {
                        if let Some(value) = selected_completion(&app, &picker)
                            .or_else(|| first_matching_completion(&app, &prompt))
                            .and_then(|suggestion| complete_prompt(&prompt, suggestion))
                        {
                            prompt = value;
                            prompt_cursor = command_prompt_cursor(&prompt);
                            picker.close();
                            dirty = true;
                        } else if let Some(value) = structural_complete_prompt(&prompt) {
                            prompt = value;
                            prompt_cursor = command_prompt_cursor(&prompt);
                            picker.close();
                            dirty = true;
                        }
                    }
                    KeyCode::Left => {
                        move_prompt_cursor_left(&prompt, &mut prompt_cursor);
                        dirty = true;
                    }
                    KeyCode::Right => {
                        move_prompt_cursor_right(&prompt, &mut prompt_cursor);
                        dirty = true;
                    }
                    KeyCode::Up => {
                        if picker.open {
                            picker.previous(app.suggestions().len());
                            dirty = true;
                            continue;
                        }
                        if active_target_prefix(&prompt).is_some()
                            && first_matching_completion(&app, &prompt).is_some()
                        {
                            picker.open();
                            dirty = true;
                            continue;
                        }
                        let history = app.history();
                        if !history.is_empty() {
                            let index = history_index
                                .map(|index| index.saturating_sub(1))
                                .unwrap_or(history.len() - 1);
                            history_index = Some(index);
                            prompt = history[index].clone();
                            prompt_cursor = prompt_len(&prompt);
                            dirty = true;
                        }
                    }
                    KeyCode::Down => {
                        if picker.open {
                            picker.next(app.suggestions().len());
                            dirty = true;
                            continue;
                        }
                        if let Some(index) = history_index {
                            let next = index + 1;
                            if next < app.history().len() {
                                history_index = Some(next);
                                prompt = app.history()[next].clone();
                                prompt_cursor = prompt_len(&prompt);
                            } else {
                                history_index = None;
                                prompt = initial_prompt();
                                prompt_cursor = initial_prompt_cursor();
                            }
                            dirty = true;
                        }
                    }
                    _ => {}
                },
                _ => {}
            }
        }
    }
    Ok(())
}

fn current_max_scroll(
    terminal: &mut Terminal<CrosstermBackend<std::io::Stdout>>,
    app: &App,
    picker: &CompletionPicker,
    transcript_cache: &mut TranscriptCache,
    options: UiOptions,
) -> Result<usize> {
    let size = terminal.size()?;
    let transcript_height = size
        .height
        .saturating_sub(suggestion_height(app, picker))
        .saturating_sub(3)
        .saturating_sub(1) as usize;
    let lines = transcript_cache.render_with_diff(app.results(), size.width as usize, options.diff);
    Ok(max_scroll(lines.len(), transcript_height))
}

fn suggest_if_target(broker_in: &mut ChildStdin, prompt: &str) -> Result<()> {
    if active_target_prefix(prompt).is_some() {
        send(
            broker_in,
            &BrokerRequest::Suggest {
                line: prompt.to_string(),
            },
        )?;
    }
    Ok(())
}

fn send(writer: &mut ChildStdin, request: &BrokerRequest) -> Result<()> {
    serde_json::to_writer(&mut *writer, request)?;
    writer.write_all(b"\n")?;
    writer.flush()?;
    Ok(())
}

fn prompt_len(prompt: &str) -> usize {
    prompt.chars().count()
}

fn prompt_byte_index(prompt: &str, cursor: usize) -> usize {
    prompt
        .char_indices()
        .nth(cursor)
        .map(|(index, _)| index)
        .unwrap_or(prompt.len())
}

fn min_prompt_cursor(prompt: &str) -> usize {
    let _ = prompt;
    0
}

fn move_prompt_cursor_left(prompt: &str, cursor: &mut usize) {
    let spans = prompt_editable_spans(prompt);
    if spans.is_empty() {
        return;
    }
    let current = (*cursor).min(prompt_len(prompt));
    for (index, span) in spans.iter().enumerate().rev() {
        if current > span.end {
            *cursor = span.end;
            return;
        }
        if current > span.start && current <= span.end {
            *cursor = current - 1;
            return;
        }
        if current == span.start {
            if index > 0 {
                *cursor = spans[index - 1].end;
            } else {
                *cursor = span.start;
            }
            return;
        }
    }
    *cursor = spans[0].start;
}

fn move_prompt_cursor_right(prompt: &str, cursor: &mut usize) {
    let spans = prompt_editable_spans(prompt);
    if spans.is_empty() {
        return;
    }
    let current = (*cursor).min(prompt_len(prompt));
    for (index, span) in spans.iter().enumerate() {
        if current < span.start {
            *cursor = span.start;
            return;
        }
        if current < span.end {
            *cursor = current + 1;
            return;
        }
        if current == span.end {
            if let Some(next) = spans.get(index + 1) {
                *cursor = next.start;
            } else {
                *cursor = span.end;
            }
            return;
        }
    }
    *cursor = spans.last().map(|span| span.end).unwrap_or(current);
}

fn insert_prompt_char(prompt: &mut String, cursor: &mut usize, ch: char) {
    *cursor = (*cursor).clamp(min_prompt_cursor(prompt), prompt_len(prompt));
    if ch == ',' && insert_target_separator(prompt, cursor) {
        return;
    }
    if !prompt_can_insert_at(prompt, *cursor) {
        return;
    }
    let index = prompt_byte_index(prompt, *cursor);
    prompt.insert(index, ch);
    *cursor += 1;
}

fn insert_target_separator(prompt: &mut String, cursor: &mut usize) -> bool {
    let chars: Vec<char> = prompt.chars().collect();
    let Some((body_start, body_end)) = target_group_body_range(prompt) else {
        return false;
    };
    if *cursor <= body_start || *cursor >= body_end {
        return false;
    }
    let mut quote_start = None;
    let mut quote_end = None;
    let mut escaped = false;
    for index in body_start..body_end {
        let ch = chars[index];
        if escaped {
            escaped = false;
            continue;
        }
        if quote_start.is_some() && ch == '\\' {
            escaped = true;
            continue;
        }
        if ch != '\'' {
            continue;
        }
        if quote_start.is_none() {
            quote_start = Some(index);
            continue;
        }
        quote_end = Some(index);
        if *cursor > quote_start.unwrap() && *cursor <= index + 1 {
            break;
        }
        quote_start = None;
        quote_end = None;
    }
    let (Some(quote_start), Some(quote_end)) = (quote_start, quote_end) else {
        return false;
    };
    if *cursor > quote_end + 1 {
        return false;
    }
    let content_end = (*cursor).min(quote_end);
    let before_cursor = chars[quote_start + 1..content_end]
        .iter()
        .collect::<String>();
    if before_cursor.trim().is_empty() {
        return false;
    }
    if *cursor <= quote_end {
        let after_cursor = chars[*cursor..quote_end].iter().collect::<String>();
        if !after_cursor.trim().is_empty() {
            return false;
        }
    }
    let insert_at = prompt_byte_index(prompt, quote_end + 1);
    prompt.insert_str(insert_at, ", ''");
    *cursor = quote_end + ", ''".chars().count();
    true
}

fn target_group_body_range(prompt: &str) -> Option<(usize, usize)> {
    group_body_range(prompt, '[', ']')
}

fn command_group_body_range(prompt: &str) -> Option<(usize, usize)> {
    group_body_range(prompt, '(', ')')
}

fn group_body_range(prompt: &str, open_marker: char, close_marker: char) -> Option<(usize, usize)> {
    let chars: Vec<char> = prompt.chars().collect();
    let open = chars.iter().position(|ch| *ch == open_marker)?;
    let mut in_quote = false;
    let mut escaped = false;
    for index in open + 1..chars.len() {
        let ch = chars[index];
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
        if !in_quote && ch == close_marker {
            return Some((open + 1, index));
        }
    }
    None
}

fn backspace_prompt_char(prompt: &mut String, cursor: &mut usize) {
    let min = min_prompt_cursor(prompt);
    if *cursor <= min {
        return;
    }
    *cursor = (*cursor).min(prompt_len(prompt));
    if !prompt_editable_char(prompt, cursor.saturating_sub(1)) {
        return;
    }
    let start = prompt_byte_index(prompt, *cursor - 1);
    let end = prompt_byte_index(prompt, *cursor);
    prompt.replace_range(start..end, "");
    *cursor -= 1;
}

fn delete_prompt_char(prompt: &mut String, cursor: &mut usize) {
    *cursor = (*cursor).clamp(min_prompt_cursor(prompt), prompt_len(prompt));
    if !prompt_editable_char(prompt, *cursor) {
        return;
    }
    let start = prompt_byte_index(prompt, *cursor);
    let end = prompt_byte_index(prompt, *cursor + 1);
    prompt.replace_range(start..end, "");
}

fn prompt_can_insert_at(prompt: &str, cursor: usize) -> bool {
    if cursor > prompt_len(prompt) {
        return false;
    }
    prompt_editable_spans(prompt)
        .iter()
        .any(|span| cursor >= span.start && cursor <= span.end)
}

fn prompt_editable_char(prompt: &str, cursor: usize) -> bool {
    let Some(ch) = prompt.chars().nth(cursor) else {
        return false;
    };
    ch != '\'' && prompt_can_insert_at(prompt, cursor)
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
struct EditableSpan {
    start: usize,
    end: usize,
}

fn prompt_editable_spans(prompt: &str) -> Vec<EditableSpan> {
    let chars: Vec<char> = prompt.chars().collect();
    let mut spans = Vec::new();
    if let Some((start, end)) = target_group_body_range(prompt) {
        spans.extend(quoted_editable_spans(&chars, start, end));
    }
    if target_group_has_content(prompt) {
        if let Some((start, end)) = command_group_body_range(prompt) {
            spans.extend(quoted_editable_spans(&chars, start, end));
        }
    }
    spans
}

fn target_group_has_content(prompt: &str) -> bool {
    let chars: Vec<char> = prompt.chars().collect();
    let Some((start, end)) = target_group_body_range(prompt) else {
        return false;
    };
    quoted_editable_spans(&chars, start, end)
        .into_iter()
        .any(|span| {
            chars[span.start..span.end]
                .iter()
                .collect::<String>()
                .trim()
                != ""
        })
}

fn quoted_editable_spans(chars: &[char], start: usize, end: usize) -> Vec<EditableSpan> {
    let mut spans = Vec::new();
    let mut quote_start = None;
    let mut escaped = false;
    for index in start..end {
        let ch = chars[index];
        if escaped {
            escaped = false;
            continue;
        }
        if quote_start.is_some() && ch == '\\' {
            escaped = true;
            continue;
        }
        if ch != '\'' {
            continue;
        }
        if let Some(open) = quote_start {
            spans.push(EditableSpan {
                start: open + 1,
                end: index,
            });
            quote_start = None;
        } else {
            quote_start = Some(index);
        }
    }
    spans
}

fn draw(
    frame: &mut Frame<'_>,
    app: &App,
    picker: &CompletionPicker,
    transcript_cache: &mut TranscriptCache,
    scroll: usize,
    options: UiOptions,
    prompt_cursor: usize,
) {
    let chunks = Layout::default()
        .direction(Direction::Vertical)
        .constraints([
            Constraint::Min(1),
            Constraint::Length(suggestion_height(app, picker)),
            Constraint::Length(3),
            Constraint::Length(1),
        ])
        .split(frame.area());

    let transcript =
        transcript_cache.render_with_diff(app.results(), chunks[0].width as usize, options.diff);
    let visible = visible_lines(transcript, scroll, chunks[0].height as usize);
    frame.render_widget(transcript_view(visible), chunks[0]);

    let suggestions = suggestion_lines(app, picker);
    frame.render_widget(Paragraph::new(suggestions), chunks[1]);

    let (display_prompt, suggestion, display_cursor) =
        prompt_display_state(app.prompt(), app.suggestions(), picker, prompt_cursor);
    let prompt = Paragraph::new(prompt_line(&display_prompt, &suggestion))
        .block(Block::default().borders(Borders::ALL));
    frame.render_widget(prompt, chunks[2]);
    if chunks[2].width > 2 && chunks[2].height > 2 {
        let inner_width = chunks[2].width.saturating_sub(2) as usize;
        let column = prompt_cursor_column(&display_prompt, &suggestion, display_cursor)
            .min(inner_width.saturating_sub(1)) as u16;
        frame.set_cursor_position(Position {
            x: chunks[2].x + 1 + column,
            y: chunks[2].y + 1,
        });
    }

    frame.render_widget(
        Paragraph::new(Line::from(Span::styled(
            app.status_line(),
            Style::default().fg(Color::Yellow),
        ))),
        chunks[3],
    );
}

fn prompt_display_state(
    prompt: &str,
    suggestions: &[String],
    picker: &CompletionPicker,
    prompt_cursor: usize,
) -> (String, String, usize) {
    if picker.open {
        if let Some(preview) =
            selected_prompt_preview(prompt, suggestions, picker.checked_indices())
        {
            let cursor = command_prompt_cursor(&preview);
            return (preview, String::new(), cursor);
        }
    }
    let suggestion = if active_target_prefix(prompt).is_some() {
        inline_suggestion(
            prompt,
            suggestions,
            picker.selected_index(suggestions.len()),
        )
    } else {
        String::new()
    };
    (prompt.to_string(), suggestion, prompt_cursor)
}

fn suggestion_height(app: &App, picker: &CompletionPicker) -> u16 {
    if !picker.open || app.suggestions().is_empty() {
        0
    } else {
        app.suggestions().len().min(8) as u16
    }
}

fn suggestion_lines(app: &App, picker: &CompletionPicker) -> Vec<Line<'static>> {
    if !picker.open || app.suggestions().is_empty() {
        return Vec::new();
    }
    app.suggestions()
        .iter()
        .take(8)
        .enumerate()
        .map(|(index, suggestion)| {
            let marker = if index == picker.selected { "> " } else { "  " };
            let checked = if picker.is_checked(index) {
                "[x] "
            } else {
                "[ ] "
            };
            Line::from(vec![
                Span::styled(marker, Style::default().fg(Color::Gray)),
                Span::styled(checked, Style::default().fg(Color::Gray)),
                Span::styled(suggestion.clone(), Style::default().fg(Color::Cyan)),
            ])
        })
        .collect()
}

fn selected_completion<'a>(app: &'a App, picker: &CompletionPicker) -> Option<&'a String> {
    picker
        .selected_index(app.suggestions().len())
        .and_then(|index| app.suggestions().get(index))
}

fn first_matching_completion<'a>(app: &'a App, prompt: &str) -> Option<&'a String> {
    app.suggestions()
        .iter()
        .find(|suggestion| complete_prompt(prompt, suggestion).is_some())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn broker_command_prefers_repo_local_go_source() {
        let command = broker_command();
        let args: Vec<_> = command
            .get_args()
            .map(|arg| arg.to_string_lossy().into_owned())
            .collect();

        assert_eq!(command.get_program(), "go");
        assert_eq!(args, ["run", "./cmd/nssh", "repl", "broker", "--json"]);
        assert!(command
            .get_current_dir()
            .is_some_and(|dir| dir.join("go.mod").exists()));
    }

    #[test]
    fn initial_prompt_starts_at_target_prefix() {
        assert_eq!(initial_prompt(), "[ '' ] ( '' )");
        assert_eq!(initial_prompt_cursor(), 3);
    }

    #[test]
    fn options_enable_diff_only_with_flag() {
        assert!(!options_from_args(["nssh-repl-ratatui"]).diff);
        assert!(options_from_args(["nssh-repl-ratatui", "--diff"]).diff);
    }

    #[test]
    fn options_default_to_pipe_cursor_and_allow_underscore() {
        assert_eq!(
            options_from_args(["nssh-repl-ratatui"]).cursor,
            PromptCursor::Pipe
        );
        assert_eq!(
            options_from_args(["nssh-repl-ratatui", "--cursor=underscore"]).cursor,
            PromptCursor::Underscore
        );
    }

    #[test]
    fn prompt_cursor_moves_and_edits_inside_targets() {
        let mut prompt = "[ 'edge01' ] ( 'show hostname' )".to_string();
        let mut cursor = prompt_len(&prompt);

        move_prompt_cursor_left(&prompt, &mut cursor);
        move_prompt_cursor_left(&prompt, &mut cursor);
        insert_prompt_char(&mut prompt, &mut cursor, 'X');
        backspace_prompt_char(&mut prompt, &mut cursor);

        assert_eq!(prompt, "[ 'edge01' ] ( 'show hostname' )");
        assert!(cursor < prompt_len(&prompt));

        for _ in 0..100 {
            move_prompt_cursor_left(&prompt, &mut cursor);
        }
        assert_eq!(cursor, initial_prompt_cursor());
        backspace_prompt_char(&mut prompt, &mut cursor);
        assert_eq!(prompt, "[ 'edge01' ] ( 'show hostname' )");
        assert_eq!(cursor, initial_prompt_cursor());
    }

    #[test]
    fn cursor_cannot_enter_command_field_until_target_is_filled() {
        let mut prompt = initial_prompt();
        let mut cursor = initial_prompt_cursor();

        for _ in 0..20 {
            move_prompt_cursor_right(&prompt, &mut cursor);
        }
        assert_eq!(cursor, initial_prompt_cursor());

        cursor = command_prompt_cursor(&prompt);
        insert_prompt_char(&mut prompt, &mut cursor, 's');
        assert_eq!(prompt, initial_prompt());
    }

    #[test]
    fn cursor_jumps_between_target_and_command_fields() {
        let prompt = "[ 'edge01' ] ( '' )".to_string();
        let mut cursor = "[ 'edge01".chars().count();

        move_prompt_cursor_right(&prompt, &mut cursor);
        assert_eq!(cursor, command_prompt_cursor(&prompt));

        move_prompt_cursor_left(&prompt, &mut cursor);
        assert_eq!(cursor, "[ 'edge01".chars().count());
    }

    #[test]
    fn prompt_syntax_cannot_be_deleted_or_typed_over() {
        let mut prompt = "[ 'edge01' ] ( '' )".to_string();
        let mut cursor = "[ 'edge01'".chars().count();

        backspace_prompt_char(&mut prompt, &mut cursor);
        assert_eq!(prompt, "[ 'edge01' ] ( '' )");

        cursor = 0;
        delete_prompt_char(&mut prompt, &mut cursor);
        assert_eq!(prompt, "[ 'edge01' ] ( '' )");

        insert_prompt_char(&mut prompt, &mut cursor, 'x');
        assert_eq!(prompt, "[ 'edge01' ] ( '' )");
    }

    #[test]
    fn prompt_content_remains_editable_inside_quotes() {
        let mut prompt = initial_prompt();
        let mut cursor = initial_prompt_cursor();

        insert_prompt_char(&mut prompt, &mut cursor, 'e');
        insert_prompt_char(&mut prompt, &mut cursor, 'd');
        assert_eq!(prompt, "[ 'ed' ] ( '' )");

        backspace_prompt_char(&mut prompt, &mut cursor);
        assert_eq!(prompt, "[ 'e' ] ( '' )");
    }

    #[test]
    fn comma_after_target_starts_next_quoted_target() {
        let mut prompt = "[ 'edge01' ] ( '' )".to_string();
        let mut cursor = "[ 'edge01".chars().count();

        insert_prompt_char(&mut prompt, &mut cursor, ',');

        assert_eq!(prompt, "[ 'edge01', '' ] ( '' )");
        assert_eq!(cursor, "[ 'edge01', '".chars().count());
    }

    #[test]
    fn comma_after_closing_target_quote_starts_next_quoted_target() {
        let mut prompt = "[ 'edge01' ] ( '' )".to_string();
        let mut cursor = "[ 'edge01'".chars().count();

        insert_prompt_char(&mut prompt, &mut cursor, ',');

        assert_eq!(prompt, "[ 'edge01', '' ] ( '' )");
        assert_eq!(cursor, "[ 'edge01', '".chars().count());
    }

    #[test]
    fn typing_second_manual_target_after_comma() {
        let mut prompt = "[ 'edge01' ] ( '' )".to_string();
        let mut cursor = "[ 'edge01".chars().count();

        for ch in ",edge02".chars() {
            insert_prompt_char(&mut prompt, &mut cursor, ch);
        }

        assert_eq!(prompt, "[ 'edge01', 'edge02' ] ( '' )");
    }

    #[test]
    fn checked_picker_items_preview_target_group_without_mutating_prompt() {
        let prompt = "[ 'acm' ] ( '' )";
        let suggestions = vec!["acm-lab-agg-sw1".to_string(), "acm-lab-agg-sw2".to_string()];
        let mut picker = CompletionPicker::default();
        picker.open();
        picker.toggle_selected(suggestions.len());
        picker.next(suggestions.len());
        picker.toggle_selected(suggestions.len());

        let (display_prompt, suggestion, cursor) =
            prompt_display_state(prompt, &suggestions, &picker, 5);

        assert_eq!(
            display_prompt,
            "[ 'acm-lab-agg-sw1', 'acm-lab-agg-sw2' ] ( '' )"
        );
        assert_eq!(suggestion, "");
        assert_eq!(cursor, command_prompt_cursor(&display_prompt));
        assert_eq!(prompt, "[ 'acm' ] ( '' )");
    }
}
