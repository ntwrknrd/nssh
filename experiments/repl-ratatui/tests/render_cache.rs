use nssh_repl_ratatui::app::ResultBlock;
use nssh_repl_ratatui::render::{
    max_scroll, prompt_cursor_column, prompt_line, prompt_line_with_cursor,
    prompt_line_with_cursor_at, transcript_view, visible_lines, PromptCursor, TranscriptCache,
};
use ratatui::buffer::Buffer;
use ratatui::layout::Rect;
use ratatui::style::{Color, Modifier};
use ratatui::widgets::Widget;

#[test]
fn transcript_cache_reuses_lines_for_scroll_only_redraws() {
    let results = vec![
        result(0, "edge01", "show run", "alpha\nbeta\ngamma\n"),
        result(1, "edge02", "show run", "alpha\ndelta\ngamma\n"),
    ];
    let mut cache = TranscriptCache::default();

    let first = cache.render(&results, 160).as_ptr();
    let second = cache.render(&results, 160).as_ptr();

    assert_eq!(first, second);
    assert_eq!(cache.render_count(), 1);
}

#[test]
fn transcript_cache_rerenders_when_width_changes() {
    let results = vec![
        result(0, "edge01", "show run", "alpha\nbeta\ngamma\n"),
        result(1, "edge02", "show run", "alpha\ndelta\ngamma\n"),
    ];
    let mut cache = TranscriptCache::default();

    let _ = cache.render(&results, 160);
    let _ = cache.render(&results, 120);

    assert_eq!(cache.render_count(), 2);
}

#[test]
fn repeated_two_target_batches_render_as_separate_splits() {
    let results = vec![
        result_in_batch(1, 0, "edge01", "show ip int brief", "same\nleft-one\n"),
        result_in_batch(1, 1, "edge02", "show ip int brief", "same\nright-one\n"),
        result_in_batch(2, 0, "edge01", "show version", "same\nleft-two\n"),
        result_in_batch(2, 1, "edge02", "show version", "same\nright-two\n"),
    ];
    let mut cache = TranscriptCache::default();

    let lines = cache.render(&results, 120);

    assert_eq!(
        lines[0].spans[0].content.trim(),
        "--- edge01 | show ip int brief"
    );
    assert_eq!(
        lines[0].spans[2].content.trim(),
        "--- edge02 | show ip int brief"
    );
    let second_banner = lines
        .iter()
        .find(|line| line.to_string().contains("show version"))
        .expect("second split banner");
    assert_eq!(
        second_banner.spans[0].content.trim(),
        "--- edge01 | show version"
    );
    assert_eq!(
        second_banner.spans[2].content.trim(),
        "--- edge02 | show version"
    );
}

#[test]
fn visible_lines_returns_only_requested_window() {
    let results = vec![result(
        0,
        "edge01",
        "show run",
        "one\ntwo\nthree\nfour\nfive\nsix\n",
    )];
    let mut cache = TranscriptCache::default();
    let lines = cache.render(&results, 160);

    let visible = visible_lines(lines, 3, 2);

    assert_eq!(visible.len(), 2);
    assert_eq!(visible[0].to_string(), "three");
    assert_eq!(visible[1].to_string(), "four");
}

#[test]
fn visible_lines_clamps_past_end() {
    let results = vec![result(0, "edge01", "show run", "one\ntwo\n")];
    let mut cache = TranscriptCache::default();
    let lines = cache.render(&results, 160);

    let visible = visible_lines(lines, 999, 5);

    assert_eq!(visible.len(), 3);
    assert_eq!(visible[0].to_string(), "--- edge01 | show run ---");
    assert_eq!(visible[1].to_string(), "one");
    assert_eq!(visible[2].to_string(), "two");
}

#[test]
fn max_scroll_is_zero_when_content_fits_viewport() {
    assert_eq!(max_scroll(10, 12), 0);
    assert_eq!(max_scroll(10, 10), 0);
}

#[test]
fn max_scroll_is_hidden_line_count_when_content_exceeds_viewport() {
    assert_eq!(max_scroll(25, 10), 15);
}

#[test]
fn split_line_numbers_are_dim_light_gray_with_reset_background() {
    let results = vec![
        result(0, "edge01", "show run", "alpha\nbeta\n"),
        result(1, "edge02", "show run", "alpha\ndelta\n"),
    ];
    let mut cache = TranscriptCache::default();

    let lines = cache.render_with_diff(&results, 120, true);
    let first_body_row = &lines[1];

    assert_eq!(first_body_row.spans[0].content, "   1 ");
    assert_eq!(first_body_row.spans[0].style.fg, Some(Color::Gray));
    assert!(first_body_row.spans[0]
        .style
        .add_modifier
        .contains(Modifier::DIM));
    assert_eq!(first_body_row.spans[0].style.bg, Some(Color::Reset));

    let changed_body_row = &lines[2];
    assert_eq!(changed_body_row.spans[0].content, "   2 ");
    assert_eq!(changed_body_row.spans[0].style.fg, Some(Color::Gray));
    assert_eq!(changed_body_row.spans[0].style.bg, Some(Color::Reset));
    assert_eq!(changed_body_row.spans[3].content, "   2 ");
    assert_eq!(changed_body_row.spans[3].style.fg, Some(Color::Gray));
    assert_eq!(changed_body_row.spans[3].style.bg, Some(Color::Reset));
    assert_ne!(changed_body_row.spans[1].style.bg, Some(Color::Reset));
    assert_ne!(changed_body_row.spans[4].style.bg, Some(Color::Reset));
}

#[test]
fn split_diff_background_does_not_paint_leading_indentation() {
    let results = vec![
        result(0, "edge01", "show run", "alpha\n   beta\n"),
        result(1, "edge02", "show run", "alpha\n   delta\n"),
    ];
    let mut cache = TranscriptCache::default();

    let lines = cache.render_with_diff(&results, 120, true);
    let changed_body_row = &lines[2];

    assert_eq!(changed_body_row.spans[0].content, "   2 ");
    assert_eq!(changed_body_row.spans[0].style.bg, Some(Color::Reset));
    assert_eq!(changed_body_row.spans[1].content, "   ");
    assert_eq!(changed_body_row.spans[1].style.bg, Some(Color::Reset));
    assert!(changed_body_row.spans[2].content.starts_with("beta"));
    assert_ne!(changed_body_row.spans[2].style.bg, Some(Color::Reset));
}

#[test]
fn split_diff_background_is_disabled_by_default() {
    let results = vec![
        result(0, "edge01", "show run", "alpha\nbeta\n"),
        result(1, "edge02", "show run", "alpha\ndelta\n"),
    ];
    let mut cache = TranscriptCache::default();

    let lines = cache.render(&results, 120);
    let changed_body_row = &lines[2];

    assert_eq!(changed_body_row.spans[1].style.bg, Some(Color::Reset));
    assert_eq!(changed_body_row.spans[4].style.bg, Some(Color::Reset));
}

#[test]
fn transcript_view_resets_the_whole_viewport_cells() {
    let results = vec![
        result(0, "edge01", "show run", "alpha\nbeta\n"),
        result(1, "edge02", "show run", "alpha\ndelta\n"),
    ];
    let mut cache = TranscriptCache::default();
    let visible = visible_lines(cache.render_with_diff(&results, 120, true), 0, 3);
    let mut buffer = Buffer::empty(Rect::new(0, 0, 120, 5));
    buffer.set_style(
        Rect::new(0, 0, 120, 5),
        ratatui::style::Style::default().bg(Color::Red),
    );

    for x in 0..120 {
        for y in 0..5 {
            buffer[(x, y)].set_symbol("X");
        }
    }

    transcript_view(visible).render(Rect::new(0, 0, 120, 5), &mut buffer);

    assert_eq!(buffer[(0, 2)].style().bg, Some(Color::Reset));
    assert_eq!(buffer[(4, 2)].style().bg, Some(Color::Reset));
    assert_eq!(buffer[(119, 4)].style().bg, Some(Color::Reset));
    assert_eq!(buffer[(119, 4)].symbol(), " ");
}

#[test]
fn transcript_view_preserves_four_digit_gutters_on_changed_rows() {
    let output_left = (1..=1620)
        .map(|line| {
            if line == 1619 {
                "rd 216.130.14.252:5641".to_string()
            } else {
                format!("same {line}")
            }
        })
        .collect::<Vec<_>>()
        .join("\n");
    let output_right = (1..=1620)
        .map(|line| {
            if line == 1619 {
                "rd 216.130.14.253:5641".to_string()
            } else {
                format!("same {line}")
            }
        })
        .collect::<Vec<_>>()
        .join("\n");
    let results = vec![
        result(0, "edge01", "show run", &output_left),
        result(1, "edge02", "show run", &output_right),
    ];
    let mut cache = TranscriptCache::default();
    let visible = visible_lines(cache.render_with_diff(&results, 120, true), 1619, 1);
    let mut buffer = Buffer::empty(Rect::new(0, 0, 120, 1));

    transcript_view(visible).render(Rect::new(0, 0, 120, 1), &mut buffer);

    let gutter = (0..5).map(|x| buffer[(x, 0)].symbol()).collect::<String>();
    assert_eq!(gutter, "1619 ");
    for x in 0..5 {
        assert_eq!(buffer[(x, 0)].style().fg, Some(Color::Gray));
        assert!(buffer[(x, 0)].style().add_modifier.contains(Modifier::DIM));
        assert_eq!(buffer[(x, 0)].style().bg, Some(Color::Reset));
    }
}

#[test]
fn prompt_line_renders_inline_suggestion_dimmed() {
    let line = prompt_line("[ 'acm-lab' ] ( '' )", "-agg-sw1");

    assert_eq!(line.to_string(), "nssh> [ 'acm-lab-agg-sw1' ] ( '' )");
    assert!(line
        .spans
        .iter()
        .any(|span| span.content == "-agg-sw1" && span.style.fg == Some(Color::DarkGray)));
}

#[test]
fn prompt_line_with_cursor_renders_pipe_by_default() {
    let line = prompt_line_with_cursor("[ 'edge01' ] ( 'show hostname' )", "", PromptCursor::Pipe);

    assert_eq!(line.to_string(), "nssh> [ 'edge01' ] ( 'show hostname' )");
}

#[test]
fn prompt_line_with_cursor_can_render_inside_target_group() {
    let line = prompt_line_with_cursor_at(
        "[ 'edge01' ] ( 'show hostname' )",
        "",
        PromptCursor::Pipe,
        5,
    );

    assert_eq!(line.to_string(), "nssh> [ 'edge01' ] ( 'show hostname' )");
    assert_eq!(
        prompt_cursor_column("[ 'edge01' ] ( 'show hostname' )", "", 5),
        "nssh> [ 'ed".len()
    );
}

#[test]
fn prompt_line_with_cursor_renders_underscore_when_configured() {
    let line = prompt_line_with_cursor(
        "[ 'edge01' ] ( 'show hostname' )",
        "",
        PromptCursor::Underscore,
    );

    assert_eq!(line.to_string(), "nssh> [ 'edge01' ] ( 'show hostname' )");
}

#[test]
fn prompt_cursor_column_tracks_command_end_without_inserted_cell() {
    assert_eq!(
        prompt_cursor_column(
            "[ 'edge01' ] ( 'show hostname' )",
            "",
            "[ 'edge01' ] ( 'show hostname".len()
        ),
        "nssh> [ 'edge01' ] ( 'show hostname".len()
    );
}

#[test]
fn prompt_line_shows_example_when_only_target_prefix_is_present() {
    let line = prompt_line("[ '' ] ( '' )", "");

    assert_eq!(line.to_string(), "nssh> ['TARGET'] ('COMMAND')");
    assert!(line.spans.iter().any(|span| span.content == "["
        && span.style.fg == Some(Color::Gray)
        && span.style.add_modifier.contains(Modifier::DIM)));
    assert!(line.spans.iter().any(|span| span.content == "TARGET"
        && span.style.fg == Some(Color::Cyan)
        && span.style.add_modifier.contains(Modifier::DIM)));
    assert!(line.spans.iter().any(|span| span.content == "COMMAND"
        && span.style.fg == Some(Color::LightGreen)
        && span.style.add_modifier.contains(Modifier::DIM)));
}

#[test]
fn prompt_cursor_column_tracks_starter_target_placeholder() {
    assert_eq!(
        prompt_cursor_column("[ '' ] ( '' )", "", 3),
        "nssh> ['".len()
    );
}

#[test]
fn prompt_line_groups_targets_and_styles_command_tokens() {
    let line = prompt_line("[ 'edge01', 'edge02' ] ( 'show hostname --json' )", "");

    assert_eq!(
        line.to_string(),
        "nssh> [ 'edge01', 'edge02' ] ( 'show hostname --json' )"
    );
    assert!(line.spans.iter().any(|span| span.content == "["
        && span.style.fg == Some(Color::Gray)
        && !span.style.add_modifier.contains(Modifier::DIM)));
    assert!(line.spans.iter().any(|span| span.content == "e"
        && span.style.fg == Some(Color::Cyan)
        && span.style.add_modifier.contains(Modifier::BOLD)));
    assert!(line
        .spans
        .iter()
        .any(|span| span.content == "s" && span.style.fg == Some(Color::LightGreen)));
    assert!(line
        .spans
        .iter()
        .any(|span| span.content == "-" && span.style.fg == Some(Color::Yellow)));
}

#[test]
fn prompt_line_keeps_incomplete_target_group_dimmed() {
    let line = prompt_line("[ 'acm-lab' ] ( '' )", "-agg-sw1");

    assert!(line.spans.iter().any(|span| span.content == "["
        && span.style.fg == Some(Color::Gray)
        && span.style.add_modifier.contains(Modifier::DIM)));
}

#[test]
fn prompt_line_opens_command_group_after_target_space() {
    let line = prompt_line("[ 'edge01' ] ( '' )", "");

    assert_eq!(line.to_string(), "nssh> [ 'edge01' ] ( '' )");
    assert!(line.spans.iter().any(|span| span.content == "["
        && span.style.fg == Some(Color::Gray)
        && !span.style.add_modifier.contains(Modifier::DIM)));
    assert!(line.spans.iter().any(|span| span.content == "("
        && span.style.fg == Some(Color::Gray)
        && span.style.add_modifier.contains(Modifier::DIM)));
}

#[test]
fn prompt_line_renders_quoted_multi_command_group() {
    let line = prompt_line(
        "[ 'edge01', 'edge02' ] ( 'show ip int brief', 'show version' )",
        "",
    );

    assert_eq!(
        line.to_string(),
        "nssh> [ 'edge01', 'edge02' ] ( 'show ip int brief', 'show version' )"
    );
    assert!(line.spans.iter().any(|span| span.content == "("
        && span.style.fg == Some(Color::Gray)
        && !span.style.add_modifier.contains(Modifier::DIM)));
    assert!(line
        .spans
        .iter()
        .any(|span| span.content == "s" && span.style.fg == Some(Color::LightGreen)));
    assert!(line
        .spans
        .iter()
        .any(|span| span.content == "v" && span.style.fg == Some(Color::LightGreen)));
}

fn result(index: usize, host: &str, command: &str, output: &str) -> ResultBlock {
    result_in_batch(0, index, host, command, output)
}

fn result_in_batch(
    batch: usize,
    index: usize,
    host: &str,
    command: &str,
    output: &str,
) -> ResultBlock {
    ResultBlock {
        batch,
        index,
        host: host.to_string(),
        command: command.to_string(),
        output: output.to_string(),
        exit_code: 0,
        error: String::new(),
    }
}
