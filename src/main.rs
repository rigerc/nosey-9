#[cfg(all(test, feature = "desktop"))]
extern crate gpui_kit as gpui;
mod catalog;
#[cfg(feature = "desktop")]
mod desktop;
mod install;
mod providers;
mod scan;
mod source;
mod workspace;

use crate::{
    catalog::Catalog,
    workspace::{Workspace, field},
};
use anyhow::{Context, Result, ensure};
use clap::{Parser, Subcommand};
use serde_json::json;
use std::{
    io::{self, IsTerminal, Write},
    path::PathBuf,
};

#[derive(Parser)]
#[command(
    version,
    about = "Discover and manage agent skills. Run without a command to open the desktop app."
)]
struct Cli {
    #[arg(long, default_value = ".", global = true)]
    cwd: PathBuf,
    #[arg(long, global = true)]
    config: Option<PathBuf>,
    /// Use this local registry instead of configured remote registries.
    #[arg(long, global = true)]
    registry: Option<PathBuf>,
    #[command(subcommand)]
    command: Option<Command>,
}
#[derive(Subcommand)]
enum Command {
    /// Detect project technologies and recommended skills.
    Scan {
        #[arg(long)]
        json: bool,
    },
    /// Search skills by name, ID, repository, technology or tag.
    Search {
        #[arg(default_value = "")]
        query: String,
        #[arg(long)]
        json: bool,
    },
    /// List project skills and tracked locations.
    List {
        #[arg(long)]
        json: bool,
        #[arg(long)]
        check_updates: bool,
    },
    /// Install a selection and its required skills.
    Install {
        #[arg(required = true)]
        skills: Vec<String>,
        #[arg(long)]
        agent: Vec<String>,
        #[arg(long)]
        copy: bool,
        #[arg(long)]
        yes: bool,
    },
    /// Refresh tracked skills from their sources.
    Update {
        skills: Vec<String>,
        #[arg(long)]
        yes: bool,
    },
    /// Remove selected tracked skills from the project.
    Uninstall {
        #[arg(required = true)]
        skills: Vec<String>,
        #[arg(long)]
        yes: bool,
    },
    /// Inspect and edit JSON settings.
    Config {
        #[command(subcommand)]
        action: Config,
    },
    /// Check workspace and catalog availability.
    Doctor {
        #[arg(long)]
        json: bool,
    },
}
#[derive(Subcommand)]
enum Config {
    Get {
        key: Option<String>,
    },
    Set {
        key: String,
        value: String,
        #[arg(long)]
        project: bool,
    },
}
fn confirm(yes: bool, text: &str) -> Result<bool> {
    if yes {
        return Ok(true);
    }
    ensure!(
        io::stdin().is_terminal(),
        "Pass --yes to confirm this operation without a terminal"
    );
    eprint!("{text}\nContinue? [y/N] ");
    io::stderr().flush()?;
    let mut answer = String::new();
    io::stdin().read_line(&mut answer)?;
    Ok(matches!(answer.trim().to_lowercase().as_str(), "y" | "yes"))
}
fn print_json(value: &impl serde::Serialize) -> Result<()> {
    println!("{}", serde_json::to_string_pretty(value)?);
    Ok(())
}
fn run() -> Result<()> {
    let cli = Cli::parse();
    let ws = Workspace::open(&cli.cwd, cli.config, cli.registry)?;
    let Some(cmd) = cli.command else {
        #[cfg(feature = "desktop")]
        {
            return desktop::open(ws);
        }
        #[cfg(not(feature = "desktop"))]
        {
            anyhow::bail!("Build with the default desktop feature to open the window");
        }
    };
    if let Command::Config { action } = cmd {
        return match action {
            Config::Get { key } => {
                let settings = ws.settings()?;
                let mut v = &settings;
                if let Some(k) = &key {
                    for seg in k.split('.') {
                        v = v.get(seg).context("Unknown setting")?;
                    }
                }
                print_json(v)
            }
            Config::Set {
                key,
                value,
                project,
            } => ws.set(&key, serde_json::from_str(&value)?, project),
        };
    }
    let mut c = Catalog::load(&ws, false)?;
    match cmd {
        Command::Scan { json } => {
            let result = scan::scan(&ws, &c)?;
            if json {
                print_json(&result)?;
            } else {
                for (id, paths) in result.technologies {
                    println!("{id}\t{}", paths.into_iter().collect::<Vec<_>>().join(", "));
                }
                for id in result.recommendations.keys() {
                    println!("Recommended\t{id}");
                }
                for w in result.warnings {
                    eprintln!("{w}");
                }
            }
        }
        Command::Search { query, json } => {
            let skills = c.search(&query);
            if json {
                print_json(&skills)?;
            } else {
                for v in skills {
                    println!("{}\t{}", field(v, "id"), field(v, "name"));
                }
            }
        }
        Command::List {
            json,
            check_updates,
        } => {
            let mut items = install::inventory(&ws, &c)?;
            if check_updates {
                install::check_updates(&ws, &mut c, &mut items)?;
            }
            if json {
                print_json(&items)?;
            } else {
                for i in items {
                    println!(
                        "{}\t{}\t{}",
                        i.name,
                        if !i.tracked {
                            "untracked"
                        } else if i.locations.is_empty() {
                            "missing"
                        } else if i.outdated {
                            "outdated"
                        } else {
                            "installed"
                        },
                        i.locations.into_iter().collect::<Vec<_>>().join(", ")
                    );
                }
            }
        }
        Command::Install {
            skills,
            agent,
            copy,
            yes,
        } => {
            let ids = install::resolve_targets(&ws, &mut c, &skills)?;
            let order = install::selection(&c, &ids, &install::inventory(&ws, &c)?)?;
            if confirm(
                yes,
                &format!("Install {} in {}", order.join(", "), ws.root.display()),
            )? {
                for id in install::install(&ws, &c, &ids, &agent, copy, false)? {
                    println!("Installed {id}");
                }
            }
        }
        Command::Update { skills, yes } => {
            let mut items = install::inventory(&ws, &c)?;
            if !skills.is_empty() {
                for target in &skills {
                    ensure!(
                        items.iter().any(|i| i.tracked
                            && (&i.name == target
                                || i.catalog_id.as_ref() == Some(target)
                                || &i.source == target)),
                        "Unknown tracked target {target}"
                    );
                }
                items.retain(|i| {
                    skills.contains(&i.name)
                        || skills.contains(&i.source)
                        || i.catalog_id.as_ref().is_some_and(|id| skills.contains(id))
                });
            }
            install::check_updates(&ws, &mut c, &mut items)?;
            let ids: Vec<_> = items
                .into_iter()
                .filter(|i| i.outdated)
                .filter_map(|i| i.catalog_id)
                .collect();
            if ids.is_empty() {
                println!("All tracked skills are current.");
            } else if confirm(yes, &format!("Update {}", ids.join(", ")))? {
                for id in install::install(&ws, &c, &ids, &[], false, true)? {
                    println!("Updated {id}");
                }
            }
        }
        Command::Uninstall { skills, yes } => {
            if confirm(
                yes,
                &format!("Remove {} from {}", skills.join(", "), ws.root.display()),
            )? {
                for id in install::uninstall(&ws, &c, &skills)? {
                    println!("Removed {id}");
                }
            }
        }
        Command::Doctor { json } => {
            let v = json!({"workspace":ws.root,"catalogSkills":c.skills.len(),"technologies":c.technologies.len(),"warnings":c.warnings});
            if json {
                print_json(&v)?;
            } else {
                println!(
                    "Workspace {}\n{} catalog skills",
                    ws.root.display(),
                    c.skills.len()
                );
            }
        }
        Command::Config { .. } => unreachable!(),
    };
    Ok(())
}
fn main() {
    if let Err(err) = run() {
        eprintln!("Nosey: {err:#}");
        std::process::exit(1);
    }
}

#[cfg(test)]
mod tests;
