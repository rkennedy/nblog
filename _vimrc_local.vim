if &filetype == 'go'
  " Line lengths should match what's in revive.toml.
  setlocal textwidth=120 colorcolumn=120
endif

if &filetype == 'yaml'
  setlocal textwidth=120 colorcolumn=120
endif
