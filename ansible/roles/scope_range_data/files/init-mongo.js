db = db.getSiblingDB('research');
db.experiments.insertMany([
  { experiment: 'spectral-17', owner: 'alice', status: 'running' },
  { experiment: 'vector-04', owner: 'bob', status: 'paused' }
]);
db.telemetry.insertMany([
  { source: 'sensor-a', value: 17.4, unit: 'celsius' },
  { source: 'sensor-b', value: 18.1, unit: 'celsius' }
]);
