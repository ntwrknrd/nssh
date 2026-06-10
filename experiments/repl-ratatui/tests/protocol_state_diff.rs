use nssh_repl_ratatui::app::{App, BrokerStatus, ResultBlock};
use nssh_repl_ratatui::diff::{align_diff, can_split, wrap_body, DiffKind};
use nssh_repl_ratatui::protocol::{BrokerEvent, BrokerRequest};

#[test]
fn request_serializes_to_broker_ndjson_shape() {
    let json = serde_json::to_string(&BrokerRequest::Suggest {
        line: "[ 'ed' ] ( '' )".to_string(),
    })
    .unwrap();

    assert_eq!(json, r#"{"type":"suggest","line":"[ 'ed' ] ( '' )"}"#);
}

#[test]
fn completed_event_decodes_base64_stdout() {
    let event: BrokerEvent = serde_json::from_str(
        r#"{"type":"completed","batch":1,"index":0,"host":"edge01","command":"show hostname","stdout":"ZWRnZTAxCg==","exit_code":0,"error":""}"#,
    )
    .unwrap();

    match event {
        BrokerEvent::Completed { batch, stdout, .. } => {
            assert_eq!(batch, 1);
            assert_eq!(stdout, b"edge01\n");
        }
        other => panic!("unexpected event: {other:?}"),
    }
}

#[test]
fn app_applies_status_completion_and_history_events() {
    let mut app = App::default();

    app.apply(BrokerEvent::History {
        lines: vec!["[ 'edge01' ] ( 'show hostname' )".to_string()],
    });
    app.apply(BrokerEvent::Status {
        running: 1,
        done: 0,
        failed: 0,
        pending: 1,
        total: 2,
    });
    app.apply(BrokerEvent::Completed {
        batch: 1,
        index: 0,
        host: "edge01".to_string(),
        command: "show hostname".to_string(),
        stdout: b"edge01\n".to_vec(),
        exit_code: 0,
        error: String::new(),
    });

    assert_eq!(app.history(), &["[ 'edge01' ] ( 'show hostname' )"]);
    assert_eq!(
        app.status(),
        BrokerStatus {
            running: 1,
            done: 0,
            failed: 0,
            pending: 1,
            total: 2
        }
    );
    assert_eq!(
        app.results(),
        &[ResultBlock {
            batch: 1,
            index: 0,
            host: "edge01".to_string(),
            command: "show hostname".to_string(),
            output: "edge01\n".to_string(),
            exit_code: 0,
            error: String::new()
        }]
    );
}

#[test]
fn app_keeps_repeated_batches_grouped_by_batch_then_index() {
    let mut app = App::default();

    apply_completed(&mut app, 1, 1, "edge02", "show one");
    apply_completed(&mut app, 1, 0, "edge01", "show one");
    apply_batch_done(&mut app);

    apply_completed(&mut app, 2, 1, "edge02", "show two");
    apply_completed(&mut app, 2, 0, "edge01", "show two");
    apply_batch_done(&mut app);

    let keys: Vec<_> = app
        .results()
        .iter()
        .map(|result| {
            (
                result.batch,
                result.index,
                result.host.as_str(),
                result.command.as_str(),
            )
        })
        .collect();
    assert_eq!(
        keys,
        [
            (1, 0, "edge01", "show one"),
            (1, 1, "edge02", "show one"),
            (2, 0, "edge01", "show two"),
            (2, 1, "edge02", "show two"),
        ]
    );
}

#[test]
fn diff_aligns_shifted_equal_lines() {
    let left = vec![
        "hardware counter feature subinterface out".to_string(),
        "hardware counter feature subinterface in".to_string(),
    ];
    let right = vec![
        "!".to_string(),
        "hardware counter feature subinterface out".to_string(),
        "hardware counter feature subinterface in".to_string(),
    ];

    let rows = align_diff(&left, &right);

    assert_eq!(rows[0].left_kind, DiffKind::Equal);
    assert_eq!(rows[0].right_kind, DiffKind::RightOnly);
    assert_eq!(rows[1].left_kind, DiffKind::Equal);
    assert_eq!(rows[1].right_kind, DiffKind::Equal);
    assert_eq!(rows[2].left_kind, DiffKind::Equal);
    assert_eq!(rows[2].right_kind, DiffKind::Equal);
}

#[test]
fn diff_keeps_repeated_interface_stanzas_aligned() {
    let left = strings(&[
        "!",
        "interface Ethernet17",
        "   shutdown",
        "!",
        "interface Ethernet18",
        "   shutdown",
        "!",
        "interface Ethernet19",
        "   shutdown",
        "   no switchport",
        "!",
    ]);
    let right = strings(&[
        "!",
        "interface Ethernet17",
        "   shutdown",
        "!",
        "interface Ethernet18",
        "   shutdown",
        "   no switchport",
        "!",
        "interface Ethernet19",
        "   shutdown",
        "   no switchport",
        "!",
    ]);

    let rows = align_diff(&left, &right);

    let ethernet18 = rows
        .iter()
        .find(|row| row.left_text == "interface Ethernet18")
        .expect("Ethernet18 row");
    assert_eq!(ethernet18.left_kind, DiffKind::Equal);
    assert_eq!(ethernet18.right_kind, DiffKind::Equal);

    let shutdown18 = rows
        .iter()
        .find(|row| row.left_line == Some(6))
        .expect("Ethernet18 shutdown row");
    assert_eq!(shutdown18.left_kind, DiffKind::Equal);
    assert_eq!(shutdown18.right_kind, DiffKind::Equal);

    let inserted = rows
        .iter()
        .find(|row| row.right_line == Some(7))
        .expect("inserted no switchport row");
    assert_eq!(inserted.left_line, None);
    assert_eq!(inserted.right_kind, DiffKind::RightOnly);
}

#[test]
fn split_requires_two_minimum_panes_not_short_lines() {
    assert!(can_split(120, 50, 4));
    assert!(!can_split(80, 50, 4));
}

#[test]
fn wrap_body_keeps_first_line_number_and_blank_continuations() {
    let rows = wrap_body(7, "abcdefghijk", 7);

    assert_eq!(rows.len(), 2);
    assert_eq!(rows[0].line_number, Some(7));
    assert_eq!(rows[0].text, "abcdefg");
    assert_eq!(rows[1].line_number, None);
    assert_eq!(rows[1].text, "hijk");
}

fn strings(lines: &[&str]) -> Vec<String> {
    lines.iter().map(|line| (*line).to_string()).collect()
}

fn apply_batch_done(app: &mut App) {
    app.apply(BrokerEvent::Status {
        running: 0,
        done: 2,
        failed: 0,
        pending: 0,
        total: 2,
    });
}

fn apply_completed(app: &mut App, batch: usize, index: usize, host: &str, command: &str) {
    app.apply(BrokerEvent::Completed {
        batch,
        index,
        host: host.to_string(),
        command: command.to_string(),
        stdout: host.as_bytes().to_vec(),
        exit_code: 0,
        error: String::new(),
    });
}
