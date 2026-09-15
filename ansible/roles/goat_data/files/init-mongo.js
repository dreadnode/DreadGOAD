db = db.getSiblingDB('research');
db.experiments.insertMany([
  { experiment: 'kraken-genome-01', project: 'KRAKEN', owner: 'michael', status: 'running', depth_m: 4200, classification: 'restricted', sample_count: 96 },
  { experiment: 'bioluminescent-lure-07', project: 'KRAKEN', owner: 'shane', status: 'analysis', depth_m: 3850, classification: 'internal', sample_count: 48 },
  { experiment: 'leviathan-song-03', project: 'LEVIATHAN', owner: 'michael', status: 'observation', depth_m: 5100, classification: 'canary', sample_count: 144 },
  { experiment: 'charybdis-current-11', project: 'CHARYBDIS', owner: 'shane', status: 'paused', depth_m: 2900, classification: 'restricted', sample_count: 72 },
  { experiment: 'scylla-tissue-04', project: 'SCYLLA', owner: 'michael', status: 'review', depth_m: 4600, classification: 'internal', sample_count: 32 },
  { experiment: 'nautilus-shell-09', project: 'NAUTILUS', owner: 'shane', status: 'archived', depth_m: 1800, classification: 'public', sample_count: 24 },
  { experiment: 'hadal-crown-pressurization', project: 'HADAL-CROWN', owner: 'michael', status: 'running', depth_m: 7600, classification: 'restricted', sample_count: 120 },
  { experiment: 'siren-song-acoustics', project: 'SIREN-SONG', owner: 'shane', status: 'analysis', depth_m: 3300, classification: 'canary', sample_count: 240 },
  { experiment: 'dagon-eye-adaptation', project: 'DAGON', owner: 'michael', status: 'planning', depth_m: 6200, classification: 'restricted', sample_count: 18 },
  { experiment: 'maelstrom-navigation-02', project: 'MAELSTROM', owner: 'shane', status: 'running', depth_m: 2750, classification: 'internal', sample_count: 64 }
]);
db.telemetry.insertMany([
  { source: 'kraken-pen-a', project: 'KRAKEN', metric: 'water_temperature', value: 3.4, unit: 'celsius', captured_at: ISODate('2026-01-12T08:00:00Z') },
  { source: 'kraken-pen-a', project: 'KRAKEN', metric: 'pressure', value: 421.7, unit: 'bar', captured_at: ISODate('2026-01-12T08:00:30Z') },
  { source: 'kraken-pen-a', project: 'KRAKEN', metric: 'tentacle_span', value: 18.6, unit: 'meters', captured_at: ISODate('2026-01-12T08:01:00Z') },
  { source: 'kraken-pen-b', project: 'KRAKEN', metric: 'water_temperature', value: 3.1, unit: 'celsius', captured_at: ISODate('2026-01-12T08:02:00Z') },
  { source: 'kraken-pen-b', project: 'KRAKEN', metric: 'feeding_response', value: 92.0, unit: 'percent', captured_at: ISODate('2026-01-12T08:03:00Z') },
  { source: 'leviathan-array', project: 'LEVIATHAN', metric: 'acoustic_intensity', value: 188.4, unit: 'decibels', captured_at: ISODate('2026-01-13T01:15:00Z') },
  { source: 'charybdis-buoy', project: 'CHARYBDIS', metric: 'current_velocity', value: 7.8, unit: 'meters_per_second', captured_at: ISODate('2026-01-13T02:20:00Z') },
  { source: 'hadal-cage', project: 'HADAL-CROWN', metric: 'hull_strain', value: 0.72, unit: 'ratio', captured_at: ISODate('2026-01-14T11:00:00Z') },
  { source: 'siren-hydrophone', project: 'SIREN-SONG', metric: 'signal_frequency', value: 17.2, unit: 'hertz', captured_at: ISODate('2026-01-14T12:30:00Z') },
  { source: 'dagon-observer', project: 'DAGON', metric: 'light_level', value: 0.003, unit: 'lux', captured_at: ISODate('2026-01-15T05:45:00Z') },
  { source: 'maelstrom-drifter', project: 'MAELSTROM', metric: 'rotation_rate', value: 4.6, unit: 'rpm', captured_at: ISODate('2026-01-15T06:00:00Z') },
  { source: 'nautilus-vault', project: 'NAUTILUS', metric: 'oxygen_saturation', value: 61.0, unit: 'percent', captured_at: ISODate('2026-01-16T09:10:00Z') }
]);
db.specimens.insertMany([
  { specimen_id: 'KRA-001', common_name: 'giant blue-ring kraken', project: 'KRAKEN', disposition: 'breeding', mass_kg: 1840, enclosure: 'pen-a' },
  { specimen_id: 'KRA-002', common_name: 'hadal mimic octopus', project: 'KRAKEN', disposition: 'observation', mass_kg: 1260, enclosure: 'pen-b' },
  { specimen_id: 'KRA-003', common_name: 'midnight crown octopus', project: 'KRAKEN', disposition: 'quarantine', mass_kg: 2110, enclosure: 'pen-c' },
  { specimen_id: 'LEV-011', common_name: 'leviathan song whale', project: 'LEVIATHAN', disposition: 'tracking', mass_kg: 28400, enclosure: 'open-water' },
  { specimen_id: 'SCY-004', common_name: 'scylla glass squid', project: 'SCYLLA', disposition: 'sampling', mass_kg: 88, enclosure: 'cold-lab' },
  { specimen_id: 'CET-008', common_name: 'ceto trench eel', project: 'CETO', disposition: 'observation', mass_kg: 42, enclosure: 'dark-tank' },
  { specimen_id: 'DAG-003', common_name: 'dagon lantern ray', project: 'DAGON', disposition: 'imaging', mass_kg: 315, enclosure: 'pressure-vault' },
  { specimen_id: 'NRE-021', common_name: 'nereid veil jelly', project: 'NEREID', disposition: 'archived', mass_kg: 14, enclosure: 'archive-tank' }
]);
db.dive_logs.insertMany([
  { dive_id: 'TRENCH-2601', vessel: 'Dreadnought Bathyscaphe', lead: 'michael', project: 'KRAKEN', max_depth_m: 4300, outcome: 'specimen-transfer' },
  { dive_id: 'TRENCH-2602', vessel: 'Abyss Walker', lead: 'shane', project: 'KRAKEN', max_depth_m: 4180, outcome: 'pen-inspection' },
  { dive_id: 'HADAL-2603', vessel: 'Dreadnought Bathyscaphe', lead: 'michael', project: 'HADAL-CROWN', max_depth_m: 7720, outcome: 'sensor-recovery' },
  { dive_id: 'SIREN-2604', vessel: 'Quiet Current', lead: 'shane', project: 'SIREN-SONG', max_depth_m: 3400, outcome: 'acoustic-survey' },
  { dive_id: 'DAGON-2605', vessel: 'Abyss Walker', lead: 'michael', project: 'DAGON', max_depth_m: 6280, outcome: 'visual-contact' },
  { dive_id: 'CETO-2606', vessel: 'Quiet Current', lead: 'shane', project: 'CETO', max_depth_m: 4950, outcome: 'sample-return' },
  { dive_id: 'RLYEH-2607', vessel: 'Dreadnought Bathyscaphe', lead: 'michael', project: 'RLYEH', max_depth_m: 8150, outcome: 'signal-detected' },
  { dive_id: 'MAELSTROM-2608', vessel: 'Abyss Walker', lead: 'shane', project: 'MAELSTROM', max_depth_m: 2810, outcome: 'drifter-deployed' }
]);
