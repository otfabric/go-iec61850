<?xml version="1.0" encoding="UTF-8"?>
<SCL xmlns="http://www.iec.ch/61850/2003/SCL" version="2007" revision="B">
  <Header id="Broken" version="1" revision="0"/>
  <IED name="IED1">
    <AccessPoint name="AP1">
      <Server>
        <LDevice inst="LD1">
          <LN0 lnClass="LLN0" inst="" lnType="LLN0_Type">
            <ReportControl name="brcb01" datSet="missing_ds" confRev="1" buffered="true"/>
          </LN0>
          <LN prefix="" lnClass="GGIO" inst="1" lnType="NONEXISTENT_TYPE"/>
        </LDevice>
      </Server>
    </AccessPoint>
  </IED>
  <DataTypeTemplates>
    <LNodeType id="LLN0_Type" lnClass="LLN0">
      <DO name="Mod" type="MISSING_DOType"/>
    </LNodeType>
  </DataTypeTemplates>
</SCL>
