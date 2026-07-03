package lgxtypes

import (
	"encoding/binary"
	"io"
)

type PID struct {
	CTL    int32
	SP     float32
	KP     float32
	KI     float32
	KD     float32
	BIAS   float32
	MAXS   float32
	MINS   float32
	DB     float32
	SO     float32
	MAXO   float32
	MINO   float32
	UPD    float32
	PV     float32
	ERR    float32
	OUT    float32
	PVH    float32
	PVL    float32
	DVP    float32
	DVN    float32
	PVDB   float32
	DVDB   float32
	MAXI   float32
	MINI   float32
	TIE    float32
	MAXCV  float32
	MINCV  float32
	MINTIE float32
	MAXTIE float32
	DATA   [17]float32

	EN   bool // bit 31
	CT   bool // bit 30
	CL   bool // bit 29
	PVT  bool // bit 28
	DOE  bool // bit 27
	SWM  bool // bit 26
	CA   bool // bit 25
	MO   bool // bit 24
	PE   bool // bit 23
	NDF  bool // bit 22
	NOBC bool // bit 21
	NOZC bool // bit 20
	INI  bool // bit 15
	SPOR bool // bit 14
	OLL  bool // bit 13
	OLH  bool // bit 12
	EWD  bool // bit 11
	DVNA bool // bit 10
	DVPA bool // bit 9
	PVLA bool // bit 8
	PVHA bool // bit 7
}

func (p PID) Pack(w io.Writer) (int, error) {
	ctrlWord := p.CTL
	if ctrlWord == 0 {
		ctrlWord = p.controlWord()
	}
	err := binary.Write(w, binary.LittleEndian, ctrlWord)
	if err != nil {
		return 0, err
	}

	values := p.values()
	err = binary.Write(w, binary.LittleEndian, values)
	if err != nil {
		return 4, err
	}

	err = binary.Write(w, binary.LittleEndian, p.DATA)
	if err != nil {
		return 116, err
	}

	return 184, nil
}

func (p *PID) Unpack(r io.Reader) (int, error) {
	var ctrlWord int32
	err := binary.Read(r, binary.LittleEndian, &ctrlWord)
	if err != nil {
		return 0, err
	}
	p.setControlWord(ctrlWord)

	fields := []*float32{
		&p.SP, &p.KP, &p.KI, &p.KD, &p.BIAS, &p.MAXS, &p.MINS, &p.DB,
		&p.SO, &p.MAXO, &p.MINO, &p.UPD, &p.PV, &p.ERR, &p.OUT, &p.PVH,
		&p.PVL, &p.DVP, &p.DVN, &p.PVDB, &p.DVDB, &p.MAXI, &p.MINI, &p.TIE,
		&p.MAXCV, &p.MINCV, &p.MINTIE, &p.MAXTIE,
	}
	for i, field := range fields {
		err = binary.Read(r, binary.LittleEndian, field)
		if err != nil {
			return 4 + i*4, err
		}
	}

	err = binary.Read(r, binary.LittleEndian, &p.DATA)
	if err != nil {
		return 116, err
	}

	return 184, nil
}

func (PID) TypeAbbr() (string, uint16) {
	return "PID,DINT,REAL,REAL,REAL,REAL,REAL,REAL,REAL,REAL,REAL,REAL,REAL,REAL,REAL,REAL,REAL,REAL,REAL,REAL,REAL,REAL,REAL,REAL,REAL,REAL,REAL,REAL,REAL,REAL,REAL[17]", 0x0F8A
}

func (p PID) controlWord() int32 {
	var ctrlWord uint32
	if p.EN {
		ctrlWord |= 1 << 31
	}
	if p.CT {
		ctrlWord |= 1 << 30
	}
	if p.CL {
		ctrlWord |= 1 << 29
	}
	if p.PVT {
		ctrlWord |= 1 << 28
	}
	if p.DOE {
		ctrlWord |= 1 << 27
	}
	if p.SWM {
		ctrlWord |= 1 << 26
	}
	if p.CA {
		ctrlWord |= 1 << 25
	}
	if p.MO {
		ctrlWord |= 1 << 24
	}
	if p.PE {
		ctrlWord |= 1 << 23
	}
	if p.NDF {
		ctrlWord |= 1 << 22
	}
	if p.NOBC {
		ctrlWord |= 1 << 21
	}
	if p.NOZC {
		ctrlWord |= 1 << 20
	}
	if p.INI {
		ctrlWord |= 1 << 15
	}
	if p.SPOR {
		ctrlWord |= 1 << 14
	}
	if p.OLL {
		ctrlWord |= 1 << 13
	}
	if p.OLH {
		ctrlWord |= 1 << 12
	}
	if p.EWD {
		ctrlWord |= 1 << 11
	}
	if p.DVNA {
		ctrlWord |= 1 << 10
	}
	if p.DVPA {
		ctrlWord |= 1 << 9
	}
	if p.PVLA {
		ctrlWord |= 1 << 8
	}
	if p.PVHA {
		ctrlWord |= 1 << 7
	}
	return int32(ctrlWord)
}

func (p *PID) setControlWord(cw int32) {
	p.CTL = cw
	ctrlWord := uint32(cw)
	p.EN = ctrlWord&(1<<31) != 0
	p.CT = ctrlWord&(1<<30) != 0
	p.CL = ctrlWord&(1<<29) != 0
	p.PVT = ctrlWord&(1<<28) != 0
	p.DOE = ctrlWord&(1<<27) != 0
	p.SWM = ctrlWord&(1<<26) != 0
	p.CA = ctrlWord&(1<<25) != 0
	p.MO = ctrlWord&(1<<24) != 0
	p.PE = ctrlWord&(1<<23) != 0
	p.NDF = ctrlWord&(1<<22) != 0
	p.NOBC = ctrlWord&(1<<21) != 0
	p.NOZC = ctrlWord&(1<<20) != 0
	p.INI = ctrlWord&(1<<15) != 0
	p.SPOR = ctrlWord&(1<<14) != 0
	p.OLL = ctrlWord&(1<<13) != 0
	p.OLH = ctrlWord&(1<<12) != 0
	p.EWD = ctrlWord&(1<<11) != 0
	p.DVNA = ctrlWord&(1<<10) != 0
	p.DVPA = ctrlWord&(1<<9) != 0
	p.PVLA = ctrlWord&(1<<8) != 0
	p.PVHA = ctrlWord&(1<<7) != 0
}

func (p PID) values() [28]float32 {
	return [28]float32{
		p.SP, p.KP, p.KI, p.KD, p.BIAS, p.MAXS, p.MINS, p.DB,
		p.SO, p.MAXO, p.MINO, p.UPD, p.PV, p.ERR, p.OUT, p.PVH,
		p.PVL, p.DVP, p.DVN, p.PVDB, p.DVDB, p.MAXI, p.MINI, p.TIE,
		p.MAXCV, p.MINCV, p.MINTIE, p.MAXTIE,
	}
}
