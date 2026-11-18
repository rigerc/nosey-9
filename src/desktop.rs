use crate::{
    catalog::Catalog,
    install::{self, Installed},
    scan::{self, Scan},
    workspace::{Workspace, field, strings},
};
use anyhow::Result;
use gpui_kit::{assets::IconName, prelude::FluentBuilder};
use gpui_kit::{
    component::{
        ActiveTheme, Disableable, Selectable, Sizable,
        button::{Button, ButtonVariants},
        input::{Input, InputEvent, InputState},
        label::Label,
    },
    *,
};
use serde_json::Value;
use std::{collections::BTreeSet, sync::Arc};

const PAGE: usize = 24;
#[derive(Clone, Copy, PartialEq, Eq)]
enum Tab {
    Scan,
    Explore,
    Manage,
    Settings,
}
impl Tab {
    fn title(self) -> &'static str {
        match self {
            Self::Scan => "Scan",
            Self::Explore => "Explore",
            Self::Manage => "Manage",
            Self::Settings => "Settings",
        }
    }
    fn icon(self) -> IconName {
        match self {
            Self::Scan => IconName::Radar,
            Self::Explore => IconName::Compass,
            Self::Manage => IconName::Package,
            Self::Settings => IconName::Settings,
        }
    }
}
struct Snapshot {
    catalog: Arc<Catalog>,
    scan: Scan,
    items: Vec<Installed>,
}
#[derive(Clone)]
enum Action {
    Install(Vec<String>),
    Update(String),
    Remove(String),
}
struct Review {
    action: Action,
    title: String,
    lines: Vec<String>,
}
pub struct Desktop {
    ws: Workspace,
    tab: Tab,
    data: Option<Snapshot>,
    busy: bool,
    status: String,
    error: bool,
    query: Entity<InputState>,
    agent_input: Entity<InputState>,
    directory: Entity<InputState>,
    copy: bool,
    selected: BTreeSet<String>,
    page: usize,
    review: Option<Review>,
    details: bool,
    show_warnings: bool,
}
impl Desktop {
    fn new(ws: Workspace, window: &mut Window, cx: &mut Context<Self>) -> Self {
        let query = cx.new(|cx| {
            InputState::new(window, cx).placeholder("Search skills, repositories or technologies")
        });
        cx.subscribe(&query, |this, _, event: &InputEvent, cx| {
            if matches!(event, InputEvent::Change) {
                this.page = 0;
                cx.notify();
            }
        })
        .detach();
        let agent_input =
            cx.new(|cx| InputState::new(window, cx).placeholder("codex, claude-code"));
        let directory =
            cx.new(|cx| InputState::new(window, cx).placeholder("Use agent directories"));
        let mut view = Self {
            ws,
            tab: Tab::Scan,
            data: None,
            busy: false,
            status: String::new(),
            error: false,
            query,
            agent_input,
            directory,
            copy: false,
            selected: BTreeSet::new(),
            page: 0,
            review: None,
            details: false,
            show_warnings: false,
        };
        view.read_settings(window, cx);
        view.reload(false, cx);
        view
    }
    fn read_settings(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if let Ok(v) = self.ws.settings() {
            let o = &v["installation"];
            let agents = strings(&o["agents"]).collect::<Vec<_>>().join(", ");
            self.agent_input
                .update(cx, |s, cx| s.set_value(agents, window, cx));
            self.directory.update(cx, |s, cx| {
                s.set_value(field(o, "directory").to_owned(), window, cx)
            });
            self.copy = o["method"] == "copy";
        }
    }
    fn message(&mut self, message: String, error: bool, cx: &mut Context<Self>) {
        self.status = message;
        self.error = error;
        cx.notify();
    }
    fn reload(&mut self, refresh: bool, cx: &mut Context<Self>) {
        self.busy = true;
        self.message("Reading registry and scanning workspace…".into(), false, cx);
        let ws = self.ws.clone();
        let task = cx.background_executor().spawn(async move {
            let catalog = Arc::new(Catalog::load(&ws, refresh)?);
            let items = install::inventory(&ws, &catalog)?;
            let scan = scan::scan(&ws, &catalog)?;
            Ok::<_, anyhow::Error>(Snapshot {
                catalog,
                items,
                scan,
            })
        });
        cx.spawn(async move |this, cx| {
            let result = task.await;
            let _ = this.update(cx, |s, cx| {
                s.busy = false;
                match result {
                    Ok(data) => {
                        s.selected.retain(|id| data.catalog.skills.contains_key(id));
                        s.data = Some(data);
                        s.message("Scan complete".into(), false, cx);
                    }
                    Err(e) => s.message(format!("{e:#}"), true, cx),
                }
            });
        })
        .detach();
    }
    fn folder(&mut self, registry: bool, window: &mut Window, cx: &mut Context<Self>) {
        self.busy = true;
        cx.notify();
        let current = self.ws.root.clone();
        cx.spawn_in(window, async move |this, cx| {
            let path = rfd::AsyncFileDialog::new()
                .set_directory(current)
                .pick_folder()
                .await
                .map(|p| p.path().to_owned());
            let _ = this.update_in(cx, |s, window, cx| {
                s.busy = false;
                if let Some(path) = path {
                    if registry {
                        s.ws.registry = Some(path);
                    } else {
                        s.ws.root = path;
                    }
                    s.data = None;
                    s.page = 0;
                    s.selected.clear();
                    s.review = None;
                    s.query.update(cx, |q, cx| q.set_value("", window, cx));
                    s.read_settings(window, cx);
                    s.reload(false, cx);
                }
                cx.notify();
            });
        })
        .detach();
    }
    fn save(&mut self, cx: &mut Context<Self>) {
        let agents: Vec<_> = self
            .agent_input
            .read(cx)
            .value()
            .split(',')
            .map(str::trim)
            .filter(|s| !s.is_empty())
            .map(String::from)
            .collect();
        let dir = self.directory.read(cx).value().trim().to_owned();
        let result = self.ws.save_installation(&agents, &dir, self.copy);
        match result {
            Ok(()) => self.message("Project settings saved".into(), false, cx),
            Err(e) => self.message(format!("{e:#}"), true, cx),
        }
    }
    fn prepare(&mut self, action: Action, cx: &mut Context<Self>) {
        let Some(data) = &self.data else {
            return;
        };
        let result = (|| -> Result<Review> {
            let (title, lines) = match &action {
                Action::Install(ids) => {
                    let order = install::selection(&data.catalog, ids, &data.items)?;
                    let dirs = install::preview_destinations(&self.ws, &[], false)?;
                    let mut lines = vec![
                        format!("Project: {}", self.ws.root.display()),
                        format!("Destinations: {}", dirs.join(", ")),
                    ];
                    for id in order {
                        let s = &data.catalog.skills[&id];
                        lines.push(format!(
                            "{}   ·   {}",
                            install::skill_name(s),
                            source_label(s)
                        ));
                    }
                    ("Install selected skills", lines)
                }
                Action::Update(name) | Action::Remove(name) => {
                    let i = data
                        .items
                        .iter()
                        .find(|i| &i.name == name)
                        .ok_or_else(|| anyhow::anyhow!("Skill no longer installed"))?;
                    (
                        if matches!(action, Action::Update(_)) {
                            "Update skill"
                        } else {
                            "Remove skill"
                        },
                        vec![
                            name.clone(),
                            i.source.clone(),
                            i.locations
                                .iter()
                                .map(|d| self.ws.root.join(d).join(name).display().to_string())
                                .collect::<Vec<_>>()
                                .join("\n"),
                        ],
                    )
                }
            };
            Ok(Review {
                action,
                title: title.into(),
                lines,
            })
        })();
        match result {
            Ok(r) => {
                self.review = Some(r);
                cx.notify();
            }
            Err(e) => self.message(format!("{e:#}"), true, cx),
        }
    }
    fn operate(&mut self, action: Option<Action>, cx: &mut Context<Self>) {
        let Some(data) = &self.data else {
            return;
        };
        let catalog = data.catalog.clone();
        let ws = self.ws.clone();
        self.review = None;
        self.busy = true;
        self.message(
            if action.is_some() {
                "Applying changes…"
            } else {
                "Checking installed sources… "
            }
            .into(),
            false,
            cx,
        );
        let job = cx.background_executor().spawn(async move {
            let mut c = (*catalog).clone();
            let mut items = install::inventory(&ws, &c)?;
            let message = match action {
                None => {
                    install::check_updates(&ws, &mut c, &mut items)?;
                    let n = items.iter().filter(|i| i.outdated).count();
                    format!("{n} skills have updates")
                }
                Some(Action::Install(ids)) => {
                    let done = install::install(&ws, &c, &ids, &[], false, false)?;
                    items = install::inventory(&ws, &c)?;
                    format!("Installed {} skills", done.len())
                }
                Some(Action::Remove(name)) => {
                    install::uninstall(&ws, &c, std::slice::from_ref(&name))?;
                    items = install::inventory(&ws, &c)?;
                    format!("Removed {name}")
                }
                Some(Action::Update(name)) => {
                    let mut selected: Vec<_> =
                        items.iter().filter(|i| i.name == name).cloned().collect();
                    install::check_updates(&ws, &mut c, &mut selected)?;
                    let ids: Vec<_> = selected
                        .iter()
                        .filter(|i| i.outdated)
                        .filter_map(|i| i.catalog_id.clone())
                        .collect();
                    if ids.is_empty() {
                        format!("{name} is current")
                    } else {
                        install::install(&ws, &c, &ids, &[], false, true)?;
                        items = install::inventory(&ws, &c)?;
                        format!("Updated {name}")
                    }
                }
            };
            Ok::<_, anyhow::Error>((Arc::new(c), items, message))
        });
        cx.spawn(async move |this, cx| {
            let result = job.await;
            let _ = this.update(cx, |s, cx| {
                s.busy = false;
                match result {
                    Ok((c, items, msg)) => {
                        if let Some(d) = &mut s.data {
                            d.items = items;
                            d.catalog = c;
                        }
                        s.selected.clear();
                        s.message(msg, false, cx);
                    }
                    Err(e) => {
                        if let Some(d) = &mut s.data
                            && let Ok(items) = install::inventory(&s.ws, &d.catalog)
                        {
                            d.items = items;
                        }
                        s.message(format!("{e:#}"), true, cx);
                    }
                }
            });
        })
        .detach();
    }

    fn settings(&self, cx: &mut Context<Self>) -> Div {
        let registry = self
            .ws
            .registry
            .as_ref()
            .map(|p| p.display().to_string())
            .unwrap_or_else(|| "Configured registries".into());
        div()
            .flex()
            .flex_col()
            .gap_4()
            .max_w(px(690.))
            .child(Label::new("Project installation").text_lg())
            .child(
                div()
                    .child(Label::new("Target agents"))
                    .child(Input::new(&self.agent_input)),
            )
            .child(
                div()
                    .child(Label::new("Custom install directory"))
                    .child(Input::new(&self.directory)),
            )
            .child(
                Button::new("method")
                    .label(if self.copy {
                        "Method · Copy"
                    } else {
                        "Method · Symlink"
                    })
                    .on_click(cx.listener(|s, _, _, cx| {
                        s.copy = !s.copy;
                        cx.notify();
                    })),
            )
            .child(
                Button::new("save")
                    .primary()
                    .label("Save project settings")
                    .disabled(self.busy)
                    .on_click(cx.listener(|s, _, _, cx| s.save(cx))),
            )
            .child(
                div()
                    .mt_4()
                    .child(Label::new("Registry source").text_lg())
                    .child(Label::new(registry)),
            )
            .child(
                div()
                    .flex()
                    .gap_2()
                    .child(
                        Button::new("local-registry")
                            .icon(IconName::FolderOpen)
                            .label("Open local registry")
                            .disabled(self.busy)
                            .on_click(cx.listener(|s, _, w, cx| s.folder(true, w, cx))),
                    )
                    .child(
                        Button::new("configured-registry")
                            .label("Use configured registries")
                            .disabled(self.busy || self.ws.registry.is_none())
                            .on_click(cx.listener(|s, _, _, cx| {
                                s.ws.registry = None;
                                s.data = None;
                                s.selected.clear();
                                s.reload(false, cx);
                            })),
                    ),
            )
    }
    fn browse(&self, cx: &mut Context<Self>) -> Div {
        let mut body = div().flex().flex_col().gap_3();
        let Some(data) = &self.data else {
            return body;
        };
        let is_scan = self.tab == Tab::Scan;
        if is_scan {
            body = body.child(
                div()
                    .flex()
                    .items_center()
                    .gap_2()
                    .child(
                        Button::new("evidence")
                            .small()
                            .label(format!("{} technologies", data.scan.technologies.len()))
                            .selected(self.details)
                            .on_click(cx.listener(|s, _, _, cx| {
                                s.details = !s.details;
                                cx.notify();
                            })),
                    )
                    .child(Label::new(format!("{} files scanned", data.scan.files)).text_sm())
                    .when(!data.scan.warnings.is_empty(), |row| {
                        row.child(
                            Button::new("coverage")
                                .small()
                                .ghost()
                                .label("Scan diagnostics")
                                .on_click(cx.listener(|s, _, _, cx| {
                                    s.show_warnings = !s.show_warnings;
                                    cx.notify();
                                })),
                        )
                    }),
            );
            if self.details {
                for (id, files) in &data.scan.technologies {
                    body = body.child(
                        Label::new(format!(
                            "{id}   ·   {}",
                            files.iter().take(3).cloned().collect::<Vec<_>>().join(", ")
                        ))
                        .text_sm(),
                    );
                }
            }
            if self.show_warnings {
                for msg in &data.scan.warnings {
                    body = body.child(Label::new(msg.clone()).text_sm());
                }
            }
        }
        let rows: Vec<_> = data
            .catalog
            .search(&self.query.read(cx).value())
            .into_iter()
            .filter(|s| !is_scan || data.scan.recommendations.contains_key(field(s, "id")))
            .collect();
        body = body.child(
            div()
                .flex()
                .gap_2()
                .items_center()
                .child(div().flex_1().child(Input::new(&self.query)))
                .child(
                    Button::new("review")
                        .primary()
                        .label(format!("Review install ({})", self.selected.len()))
                        .disabled(self.busy || self.selected.is_empty())
                        .on_click(cx.listener(|s, _, _, cx| {
                            s.prepare(Action::Install(s.selected.iter().cloned().collect()), cx)
                        })),
                ),
        );
        body = body.child(self.pager(rows.len(), cx));
        if rows.is_empty() {
            body = body.child(Label::new(if is_scan {
                "No matching recommendations. Explore the registry to find more skills."
            } else {
                "No matching skills."
            }));
        }
        for (idx, v) in rows
            .into_iter()
            .skip(self.page * PAGE)
            .take(PAGE)
            .enumerate()
        {
            let id = field(v, "id").to_owned();
            let selected = self.selected.contains(&id);
            let source = source_label(v);
            let mut text = div()
                .min_w_0()
                .flex_1()
                .flex()
                .flex_col()
                .gap_1()
                .child(Label::new(field(v, "name").to_owned()).font_weight(FontWeight::SEMIBOLD))
                .child(
                    Label::new(source)
                        .text_sm()
                        .text_color(cx.theme().muted_foreground),
                );
            if let Some(d) = v["description"].as_str().filter(|s| !s.is_empty()) {
                text = text.child(Label::new(d.to_owned()).text_sm());
            }
            if is_scan && let Some(reasons) = data.scan.recommendations.get(&id) {
                text = text.child(
                    Label::new(reasons.iter().cloned().collect::<Vec<_>>().join(" · ")).text_sm(),
                );
            }
            body = body.child(
                div()
                    .flex()
                    .items_center()
                    .gap_3()
                    .py_2()
                    .border_b_1()
                    .border_color(cx.theme().border)
                    .child(text)
                    .child(
                        Button::new(("select", idx))
                            .small()
                            .selected(selected)
                            .label(if selected { "Selected" } else { "Select" })
                            .disabled(self.busy)
                            .on_click(cx.listener(move |s, _, _, cx| {
                                if !s.selected.remove(&id) {
                                    s.selected.insert(id.clone());
                                }
                                cx.notify();
                            })),
                    ),
            );
        }
        body
    }
    fn pager(&self, count: usize, cx: &mut Context<Self>) -> Div {
        let first = if count == 0 { 0 } else { self.page * PAGE + 1 };
        let last = ((self.page + 1) * PAGE).min(count);
        div()
            .flex()
            .items_center()
            .gap_2()
            .child(
                div().flex_1().child(
                    Label::new(format!("{first}–{last} of {count} skills"))
                        .text_sm()
                        .text_color(cx.theme().muted_foreground),
                ),
            )
            .when(count > PAGE, |row| {
                row.child(
                    Button::new("prev")
                        .small()
                        .label("Previous")
                        .disabled(self.page == 0)
                        .on_click(cx.listener(|s, _, _, cx| {
                            s.page = s.page.saturating_sub(1);
                            cx.notify();
                        })),
                )
                .child(
                    Button::new("next")
                        .small()
                        .label("Next")
                        .disabled(last == count)
                        .on_click(cx.listener(|s, _, _, cx| {
                            s.page += 1;
                            cx.notify();
                        })),
                )
            })
    }
    fn manage(&self, cx: &mut Context<Self>) -> Div {
        let mut body = div().flex().flex_col().gap_3().child(
            Button::new("check")
                .label("Check for updates")
                .icon(IconName::RefreshCw)
                .disabled(self.busy || self.data.is_none())
                .on_click(cx.listener(|s, _, _, cx| s.operate(None, cx))),
        );
        if let Some(d) = &self.data {
            if d.items.is_empty() {
                body = body.child(Label::new("No installed skills in this workspace."));
            }
            for (idx, i) in d.items.iter().enumerate() {
                let name = i.name.clone();
                let remove = name.clone();
                let state = if !i.tracked {
                    "Untracked"
                } else if i.locations.is_empty() {
                    "Missing"
                } else if i.outdated {
                    "Update available"
                } else {
                    "Installed"
                };
                body = body.child(
                    div()
                        .flex()
                        .gap_2()
                        .items_center()
                        .py_3()
                        .border_b_1()
                        .border_color(cx.theme().border)
                        .child(
                            div()
                                .flex_1()
                                .child(Label::new(i.name.clone()).font_weight(FontWeight::SEMIBOLD))
                                .child(
                                    Label::new(format!(
                                        "{state}   ·   {}",
                                        i.locations.iter().cloned().collect::<Vec<_>>().join(", ")
                                    ))
                                    .text_sm(),
                                ),
                        )
                        .when(i.tracked, |r| {
                            r.child(
                                Button::new(("update", idx))
                                    .small()
                                    .label("Update")
                                    .disabled(self.busy || i.locations.is_empty())
                                    .on_click(cx.listener(move |s, _, _, cx| {
                                        s.prepare(Action::Update(name.clone()), cx)
                                    })),
                            )
                            .child(
                                Button::new(("remove", idx))
                                    .small()
                                    .label("Remove")
                                    .disabled(self.busy)
                                    .on_click(cx.listener(move |s, _, _, cx| {
                                        s.prepare(Action::Remove(remove.clone()), cx)
                                    })),
                            )
                        }),
                );
            }
        }
        body
    }
}
fn source_label(v: &Value) -> String {
    let src = &v["source"];
    let uri = src["uri"]
        .as_str()
        .or_else(|| src["path"].as_str())
        .unwrap_or("");
    let rev = field(src, "ref");
    if rev.is_empty() {
        uri.to_owned()
    } else {
        format!(
            "{} @ {}",
            uri.trim_start_matches("https://github.com/"),
            if rev.len() == 40 && rev.is_ascii() {
                &rev[..8]
            } else {
                rev
            }
        )
    }
}
impl Render for Desktop {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = cx.theme();
        let border = theme.border;
        let foreground = theme.foreground;
        let background = theme.background;
        let muted = theme.muted_foreground;
        let sidebar = theme.sidebar;
        let mut nav = div()
            .w(px(176.))
            .flex_shrink_0()
            .h_full()
            .flex()
            .flex_col()
            .gap_2()
            .p_4()
            .bg(sidebar)
            .border_r_1()
            .border_color(border)
            .child(
                div()
                    .flex()
                    .gap_2()
                    .items_center()
                    .mb_4()
                    .child(IconName::Binoculars)
                    .child(Label::new("Nosey").text_xl().font_weight(FontWeight::BOLD)),
            );
        for tab in [Tab::Scan, Tab::Explore, Tab::Manage, Tab::Settings] {
            nav = nav.child(
                Button::new(tab.title())
                    .ghost()
                    .selected(self.tab == tab)
                    .icon(tab.icon())
                    .label(tab.title())
                    .on_click(cx.listener(move |s, _, _, cx| {
                        s.tab = tab;
                        s.page = 0;
                        s.review = None;
                        cx.notify();
                    })),
            );
        }
        let header = div()
            .flex()
            .gap_3()
            .items_center()
            .pb_4()
            .border_b_1()
            .border_color(border)
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .child(
                        Label::new(self.tab.title())
                            .text_2xl()
                            .font_weight(FontWeight::SEMIBOLD),
                    )
                    .child(
                        Label::new(self.ws.root.display().to_string())
                            .text_sm()
                            .text_color(muted),
                    ),
            )
            .child(
                Button::new("workspace")
                    .icon(IconName::FolderOpen)
                    .label("Open folder")
                    .disabled(self.busy)
                    .on_click(cx.listener(|s, _, w, cx| s.folder(false, w, cx))),
            )
            .child(
                Button::new("refresh")
                    .icon(IconName::RefreshCw)
                    .label("Refresh")
                    .disabled(self.busy)
                    .on_click(cx.listener(|s, _, _, cx| s.reload(true, cx))),
            );
        let content = if let Some(r) = &self.review {
            let action = r.action.clone();
            div()
                .flex()
                .flex_col()
                .gap_3()
                .child(Label::new(r.title.clone()).text_lg())
                .children(r.lines.iter().cloned().map(Label::new))
                .child(
                    div()
                        .flex()
                        .gap_2()
                        .child(Button::new("cancel").label("Cancel").on_click(cx.listener(
                            |s, _, _, cx| {
                                s.review = None;
                                cx.notify();
                            },
                        )))
                        .child(Button::new("confirm").primary().label("Confirm").on_click(
                            cx.listener(move |s, _, _, cx| s.operate(Some(action.clone()), cx)),
                        )),
                )
        } else {
            match self.tab {
                Tab::Settings => self.settings(cx),
                Tab::Manage => self.manage(cx),
                _ => self.browse(cx),
            }
        };
        div()
            .flex()
            .size_full()
            .bg(background)
            .text_color(foreground)
            .child(nav)
            .child(
                div()
                    .flex()
                    .flex_col()
                    .flex_1()
                    .min_w_0()
                    .h_full()
                    .gap_3()
                    .p_5()
                    .child(header)
                    .child(
                        div()
                            .id(("content", self.tab as usize))
                            .overflow_y_scroll()
                            .min_h_0()
                            .flex_1()
                            .child(content),
                    )
                    .child(
                        div().pt_2().border_t_1().border_color(border).child(
                            Label::new(self.status.clone())
                                .text_sm()
                                .text_color(if self.error { cx.theme().danger } else { muted }),
                        ),
                    ),
            )
    }
}
pub fn open(ws: Workspace) -> Result<()> {
    // Application::run owns the event loop. Propagate window creation failures
    // to the command's exit status once the loop returns.
    let error = std::rc::Rc::new(std::cell::RefCell::new(None));
    let outcome = error.clone();
    application().with_assets(assets::Assets).run(move |cx| {
        init(cx);
        cx.on_window_closed(|cx, _| {
            if cx.windows().is_empty() {
                cx.quit();
            }
        })
        .detach();
        let opts = WindowOptions {
            titlebar: Some(TitlebarOptions {
                title: Some("Nosey".into()),
                ..Default::default()
            }),
            window_bounds: Some(WindowBounds::Windowed(Bounds::centered(
                None,
                size(px(1080.), px(740.)),
                cx,
            ))),
            ..Default::default()
        };
        if let Err(e) = open_window(opts, cx, |w, cx| cx.new(|cx| Desktop::new(ws, w, cx))) {
            *outcome.borrow_mut() = Some(e);
            cx.quit();
        } else {
            cx.activate(true);
        }
    });
    if let Some(e) = error.take() {
        return Err(e);
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::Desktop;
    use gpui_kit::test::TestWindowExt;
    use gpui_kit::{
        AppContext, Bounds, TestAppContext, WindowBounds, WindowOptions, init, open_window, px,
        size,
    };
    #[gpui_kit::test]
    fn review_cancel_install_and_remove_from_a_window(cx: &mut TestAppContext) {
        let f = crate::tests::Fixture::new();
        let ws = f.ws.clone();
        let (handle, view) = cx.update(|cx| {
            init(cx);
            open_window(
                WindowOptions {
                    window_bounds: Some(WindowBounds::Windowed(Bounds {
                        origin: Default::default(),
                        size: size(px(1080.), px(740.)),
                    })),
                    ..Default::default()
                },
                cx,
                |w, cx| cx.new(|cx| Desktop::new(ws, w, cx)),
            )
            .unwrap()
        });
        cx.run_until_parked();
        cx.update_window(handle, |_, w, cx| {
            assert!(!view.read(cx).busy, "{}", view.read(cx).status);
            assert!(view.read(cx).data.is_some(), "{}", view.read(cx).status);
            w.render_frame(cx);
            w.click("Explore", cx);
            w.click(("select", 0usize), cx);
            w.click("review", cx);
            assert!(view.read(cx).review.is_some());
            w.click("cancel", cx);
            assert!(!f.ws.root.join("skills-lock.json").exists());
            w.click("review", cx);
            w.click("confirm", cx);
        })
        .unwrap();
        cx.run_until_parked();
        cx.update_window(handle, |_, w, cx| {
            assert!(
                f.ws.root.join(".agents/skills/alpha/SKILL.md").is_file(),
                "{}",
                view.read(cx).status
            );
            w.render_frame(cx);
            w.click("Manage", cx);
            w.click(("remove", 0usize), cx);
            w.click("confirm", cx);
        })
        .unwrap();
        cx.run_until_parked();
        assert!(!f.ws.root.join(".agents/skills/alpha").exists());
        assert!(f.ws.root.join(".agents/skills/beta/SKILL.md").exists());
    }
}
