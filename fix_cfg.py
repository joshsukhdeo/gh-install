import os
import glob

def replace_in_file(path):
    with open(path, 'r') as f:
        content = f.read()
    
    replacements = {
        'cfg.InstallTypes': 'cfg.Core.InstallTypes',
        'cfg.InstallPath': 'cfg.Paths.InstallPath',
        'cfg.GlobalPath': 'cfg.Paths.GlobalPath',
        'cfg.ClonePath': 'cfg.Paths.ClonePath',
        'cfg.ForkPath': 'cfg.Paths.ForkPath',
        'cfg.AICmd': 'cfg.AI.AICmd',
        'cfg.AddDeps': 'cfg.Core.AddDeps',
        'cfg.NoDeps': 'cfg.Core.NoDeps',
        'cfg.DisablePrompts': 'cfg.Core.DisablePrompts',
        'cfg.NoSaveState': 'cfg.Core.NoSaveState',
        'cfg.AllowWine': 'cfg.Core.AllowWine',
        'cfg.NativeExtract': 'cfg.Core.NativeExtract',
        'cfg.KeepSuffixes': 'cfg.Core.KeepSuffixes',
        'cfg.VTApiKey': 'cfg.Core.VTApiKey'
    }
    
    for k, v in replacements.items():
        content = content.replace(k, v)
        
    with open(path, 'w') as f:
        f.write(content)

for root, _, files in os.walk('.'):
    for file in files:
        if file.endswith('.go'):
            replace_in_file(os.path.join(root, file))
