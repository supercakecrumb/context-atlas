import { createHash } from 'node:crypto';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { basename, join } from 'node:path';

const [m49Path, naturalEarthPath, output] = process.argv.slice(2);
if (!m49Path || !naturalEarthPath || !output) {
  throw new Error('usage: node build.mjs M49_HTML NATURAL_EARTH_GEOJSON OUTPUT_DIR');
}

const sha256 = (value) => createHash('sha256').update(value).digest('hex');
const decodeNumericEntities = (value) => value.replace(/&#(x[0-9a-f]+|[0-9]+);/gi, (entity, digits) => {
  const hexadecimal = digits.slice(0, 1).toLowerCase() === 'x';
  const codePoint = Number.parseInt(hexadecimal ? digits.slice(1) : digits, hexadecimal ? 16 : 10);
  return Number.isInteger(codePoint) && codePoint >= 0 && codePoint <= 0x10ffff ? String.fromCodePoint(codePoint) : entity;
});
const text = (html) => decodeNumericEntities(html.replace(/<[^>]*>/g, '').replace(/&amp;/g, '&').replace(/&#39;/g, "'").replace(/&quot;/g, '"').replace(/&nbsp;/g, ' ').trim());
const code = (value) => value.trim().padStart(3, '0');
const sorted = (values) => [...values].sort((a, b) => a.localeCompare(b));

const m49 = await readFile(m49Path, 'utf8');
const table = m49.match(/<table id\s*=\s*"downloadTableEN"[\s\S]*?<\/table>/)?.[0]?.replace(/<!--[\s\S]*?-->/g, '');
if (!table) throw new Error('UN M49 English table was not found');

const rows = [...table.matchAll(/<tr\b[^>]*>([\s\S]*?)<\/tr>/g)].slice(1).map((row) =>
  [...row[1].matchAll(/<td[^>]*>([\s\S]*?)<\/td>/g)].map((cell) => text(cell[1])),
).filter((cells) => cells.length === 15);
if (rows.length < 200) throw new Error(`expected at least 200 M49 areas, got ${rows.length}`);

const areas = rows.map((cells) => ({
  m49: code(cells[9]),
  name: cells[8],
  iso_alpha2: cells[10] || null,
  iso_alpha3: cells[11] || null,
  region_m49: code(cells[2]),
  region_name: cells[3],
  subregion_m49: code(cells[4]),
  subregion_name: cells[5],
  intermediate_region_m49: cells[6] ? code(cells[6]) : null,
  intermediate_region_name: cells[7] || null,
  ldc: cells[12] === 'x',
  lldc: cells[13] === 'x',
  sids: cells[14] === 'x',
}));
const unique = new Set(areas.map((area) => area.m49));
if (unique.size !== areas.length) throw new Error('UN M49 source contains duplicate area codes');

const groups = new Map();
function group(id, name, kind, parentId = null) {
  if (!groups.has(id)) groups.set(id, { id, name, kind, parent_id: parentId, member_m49: new Set() });
  return groups.get(id);
}
for (const area of areas) {
  group('m49:001', 'World', 'world').member_m49.add(area.m49);
  if (area.region_m49 !== '000' && area.region_name) group(`m49:${area.region_m49}`, area.region_name, 'region', 'm49:001').member_m49.add(area.m49);
  if (area.subregion_m49 !== '000' && area.subregion_name) group(`m49:${area.subregion_m49}`, area.subregion_name, 'subregion', `m49:${area.region_m49}`).member_m49.add(area.m49);
  if (area.intermediate_region_m49 && area.intermediate_region_m49 !== '000' && area.intermediate_region_name) group(`m49:${area.intermediate_region_m49}`, area.intermediate_region_name, 'intermediate_region', `m49:${area.subregion_m49}`).member_m49.add(area.m49);
  if (area.ldc) group('un:ldc', 'Least Developed Countries (LDC)', 'ldc').member_m49.add(area.m49);
  if (area.lldc) group('un:lldc', 'Land Locked Developing Countries (LLDC)', 'lldc').member_m49.add(area.m49);
  if (area.sids) group('un:sids', 'Small Island Developing States (SIDS)', 'sids').member_m49.add(area.m49);
}
const exSoviet = ['051', '031', '112', '233', '268', '398', '417', '428', '440', '498', '643', '762', '795', '804', '860'];
for (const member of exSoviet) if (!unique.has(member)) throw new Error(`ex-Soviet member ${member} is missing from M49`);
groups.set('custom:ex_soviet', { id: 'custom:ex_soviet', name: 'ex-Soviet countries and areas', kind: 'custom', parent_id: null, member_m49: new Set(exSoviet) });

const reference = {
  schema_version: 1,
  classification: {
    name: 'UN Standard Country or Area Codes for Statistical Use (M49)',
    version_label: 'current classification applied across all historical years',
    source_url: 'https://unstats.un.org/unsd/methodology/m49/overview/',
  },
  areas,
  groups: [...groups.values()].map(({ member_m49, ...item }) => ({ ...item, member_m49: sorted(member_m49) }))
    .sort((a, b) => a.id.localeCompare(b.id)),
};

const naturalEarth = JSON.parse(await readFile(naturalEarthPath, 'utf8'));
const byISO3 = new Map(areas.filter((area) => area.iso_alpha3).map((area) => [area.iso_alpha3, area]));
const used = new Set();
const joined = new Map();
const unmatchedFeatures = [];
for (const feature of naturalEarth.features) {
  const p = feature.properties;
  const iso3 = p.ISO_A3 !== '-99' ? p.ISO_A3 : p.ISO_A3_EH;
  const area = byISO3.get(iso3) ?? null;
  if (!area) {
    unmatchedFeatures.push({ type: 'Feature', properties: {
      m49: null, name: p.NAME_EN || p.NAME || p.ADMIN,
      iso_alpha2: p.ISO_A2 !== '-99' ? p.ISO_A2 : null,
      iso_alpha3: iso3 !== '-99' ? iso3 : null, natural_earth_ids: [p.NE_ID],
    }, geometry: feature.geometry });
    continue;
  }
  used.add(area.m49);
  const item = joined.get(area.m49) ?? { area, features: [] };
  item.features.push(feature);
  joined.set(area.m49, item);
}
const features = [...joined.values()].map(({ area, features: sourceFeatures }) => {
  const geometries = sourceFeatures.map((feature) => feature.geometry);
  const geometry = geometries.length === 1 ? geometries[0] : {
    type: 'MultiPolygon',
    coordinates: geometries.flatMap((item) => item.type === 'Polygon' ? [item.coordinates] : item.coordinates),
  };
  return { type: 'Feature', properties: {
    m49: area.m49, name: area.name, iso_alpha2: area.iso_alpha2, iso_alpha3: area.iso_alpha3,
    natural_earth_ids: sourceFeatures.map((feature) => feature.properties.NE_ID).sort((a, b) => a - b),
  }, geometry };
}).concat(unmatchedFeatures);
const geometry = { type: 'FeatureCollection', name: 'natural_earth_admin_0_50m', features };
const unmatchedGeometry = features.filter((feature) => !feature.properties.m49).map((feature) => ({ name: feature.properties.name, iso_alpha3: feature.properties.iso_alpha3 }));
const unmatchedM49 = areas.filter((area) => !used.has(area.m49)).map((area) => area.m49);
reference.geometry_join = { matched_source_feature_count: naturalEarth.features.length - unmatchedFeatures.length, published_feature_count: features.length, unmatched_geometry: unmatchedGeometry, unmatched_m49: unmatchedM49 };

await mkdir(output, { recursive: true });
const referenceBytes = JSON.stringify(reference, null, 2) + '\n';
const geometryBytes = JSON.stringify(geometry) + '\n';
await writeFile(join(output, 'un-m49-current.json'), referenceBytes);
await writeFile(join(output, 'natural-earth-admin0-50m.geojson'), geometryBytes);
const checksums = {
  schema_version: 1,
  sources: [
    { file: basename(m49Path), url: reference.classification.source_url, sha256: sha256(await readFile(m49Path)) },
    { file: 'ne_50m_admin_0_countries.zip', url: 'https://naciscdn.org/naturalearth/50m/cultural/ne_50m_admin_0_countries.zip', version: '5.1.1', sha256: '5fed433373581fa648920435f937d95f2d3c0200e067409c6478dcdf1b853139', conversion: 'ogr2ogr -f GeoJSON' },
  ],
  artifacts: [
    { file: 'un-m49-current.json', sha256: sha256(referenceBytes) },
    { file: 'natural-earth-admin0-50m.geojson', sha256: sha256(geometryBytes) },
  ],
};
await writeFile(join(output, 'checksums.json'), JSON.stringify(checksums, null, 2) + '\n');
