use unicode_width::UnicodeWidthStr;

#[derive(Debug, Copy, Clone, PartialEq, Eq)]
pub enum DiffKind {
    Equal,
    LeftOnly,
    RightOnly,
    Changed,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DiffRow {
    pub left_line: Option<usize>,
    pub left_text: String,
    pub left_kind: DiffKind,
    pub right_line: Option<usize>,
    pub right_text: String,
    pub right_kind: DiffKind,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct WrappedRow {
    pub line_number: Option<usize>,
    pub text: String,
}

pub fn can_split(width: usize, min_pane_width: usize, gap_width: usize) -> bool {
    width >= gap_width && (width - gap_width) / 2 >= min_pane_width
}

pub fn align_diff(left: &[String], right: &[String]) -> Vec<DiffRow> {
    let pairs = lcs_pairs(left, right);
    let mut rows = Vec::new();
    let mut left_pos = 0;
    let mut right_pos = 0;

    for (next_left, next_right) in pairs {
        append_unmatched(
            left,
            right,
            &mut rows,
            &mut left_pos,
            next_left,
            &mut right_pos,
            next_right,
        );
        rows.push(DiffRow {
            left_line: Some(next_left + 1),
            left_text: left[next_left].clone(),
            left_kind: DiffKind::Equal,
            right_line: Some(next_right + 1),
            right_text: right[next_right].clone(),
            right_kind: DiffKind::Equal,
        });
        left_pos = next_left + 1;
        right_pos = next_right + 1;
    }
    append_unmatched(
        left,
        right,
        &mut rows,
        &mut left_pos,
        left.len(),
        &mut right_pos,
        right.len(),
    );
    rows
}

pub fn wrap_body(line_number: usize, text: &str, width: usize) -> Vec<WrappedRow> {
    let segments = wrap_text(text, width);
    segments
        .into_iter()
        .enumerate()
        .map(|(index, text)| WrappedRow {
            line_number: if index == 0 { Some(line_number) } else { None },
            text,
        })
        .collect()
}

fn append_unmatched(
    left: &[String],
    right: &[String],
    rows: &mut Vec<DiffRow>,
    left_pos: &mut usize,
    left_end: usize,
    right_pos: &mut usize,
    right_end: usize,
) {
    while *left_pos < left_end && *right_pos < right_end {
        rows.push(DiffRow {
            left_line: Some(*left_pos + 1),
            left_text: left[*left_pos].clone(),
            left_kind: DiffKind::Changed,
            right_line: Some(*right_pos + 1),
            right_text: right[*right_pos].clone(),
            right_kind: DiffKind::Changed,
        });
        *left_pos += 1;
        *right_pos += 1;
    }
    while *left_pos < left_end {
        rows.push(DiffRow {
            left_line: Some(*left_pos + 1),
            left_text: left[*left_pos].clone(),
            left_kind: DiffKind::LeftOnly,
            right_line: None,
            right_text: String::new(),
            right_kind: DiffKind::Equal,
        });
        *left_pos += 1;
    }
    while *right_pos < right_end {
        rows.push(DiffRow {
            left_line: None,
            left_text: String::new(),
            left_kind: DiffKind::Equal,
            right_line: Some(*right_pos + 1),
            right_text: right[*right_pos].clone(),
            right_kind: DiffKind::RightOnly,
        });
        *right_pos += 1;
    }
}

fn lcs_pairs(left: &[String], right: &[String]) -> Vec<(usize, usize)> {
    let mut table = vec![vec![0usize; right.len() + 1]; left.len() + 1];
    for i in (0..left.len()).rev() {
        for j in (0..right.len()).rev() {
            table[i][j] = if left[i] == right[j] {
                table[i + 1][j + 1] + 1
            } else {
                table[i + 1][j].max(table[i][j + 1])
            };
        }
    }

    let mut pairs = Vec::new();
    let mut i = 0;
    let mut j = 0;
    while i < left.len() && j < right.len() {
        if left[i] == right[j] {
            pairs.push((i, j));
            i += 1;
            j += 1;
        } else if table[i + 1][j] >= table[i][j + 1] {
            i += 1;
        } else {
            j += 1;
        }
    }
    pairs
}

fn wrap_text(text: &str, width: usize) -> Vec<String> {
    if width == 0 {
        return vec![String::new()];
    }
    if text.is_empty() {
        return vec![String::new()];
    }

    let mut rows = Vec::new();
    let mut current = String::new();
    let mut current_width = 0;
    for ch in text.chars() {
        let ch_width = ch.to_string().width().max(1);
        if current_width > 0 && current_width + ch_width > width {
            rows.push(current);
            current = String::new();
            current_width = 0;
        }
        current.push(ch);
        current_width += ch_width;
    }
    rows.push(current);
    rows
}
